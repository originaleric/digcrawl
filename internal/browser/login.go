package browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/originaleric/digcrawl/config"
	"golang.org/x/term"
)

// LoginSpec 平台登录页与 Cookie 域配置。
type LoginSpec struct {
	Platform     string
	IndexURL     string
	CookieDomain string
	CookieURLs   []string
}

var loginSpecs = map[string]LoginSpec{
	"xiaohongshu": {
		Platform:     "xiaohongshu",
		IndexURL:     "https://www.xiaohongshu.com",
		CookieDomain: "www.xiaohongshu.com",
		CookieURLs:   []string{"https://www.xiaohongshu.com"},
	},
	"douyin": {
		Platform:     "douyin",
		IndexURL:     "https://www.douyin.com",
		CookieDomain: "www.douyin.com",
		CookieURLs: []string{
			"https://douyin.com",
			"https://www.douyin.com",
			"https://creator.douyin.com",
			"https://live.douyin.com",
		},
	},
	"toutiao": {
		Platform:     "toutiao",
		IndexURL:     "https://www.toutiao.com",
		CookieDomain: "www.toutiao.com",
		CookieURLs:   []string{"https://www.toutiao.com", "https://toutiao.com"},
	},
	"threads": {
		Platform:     "threads",
		IndexURL:     "https://www.threads.com",
		CookieDomain: "www.threads.com",
		CookieURLs:   []string{"https://www.threads.com", "https://threads.com", "https://www.threads.net", "https://threads.net"},
	},
	"instagram": {
		Platform:     "instagram",
		IndexURL:     "https://www.instagram.com",
		CookieDomain: "www.instagram.com",
		CookieURLs:   []string{"https://www.instagram.com", "https://instagram.com"},
	},
}

// LoginSpecFor returns login configuration for a platform.
func LoginSpecFor(platform string) (LoginSpec, error) {
	spec, ok := loginSpecs[strings.ToLower(strings.TrimSpace(platform))]
	if !ok {
		return LoginSpec{}, fmt.Errorf("不支持的平台 %q，可选: xiaohongshu, douyin, toutiao, threads, instagram", platform)
	}
	return spec, nil
}

// InteractiveLogin 打开 headed 浏览器供用户扫码/登录，并保存 Cookie。
func InteractiveLogin(ctx context.Context, platform string, cfg config.LocalBrowserConfig) (resultErr error) {
	spec, err := LoginSpecFor(platform)
	if err != nil {
		return err
	}
	if err := requireInteractiveTerminal(os.Stdin); err != nil {
		return err
	}

	headlessFalse := false
	cfg.Headless = &headlessFalse
	cfg.ChromePath = discoverInteractiveChromePath(cfg.ChromePath)
	if cfg.ChromePath == "" {
		return fmt.Errorf("未找到可显示登录窗口的 Chrome/Chromium；安装 Chrome/Chromium 或设置 LocalBrowser.ChromePath")
	}
	cfg = normalizeLocalBrowserConfig(cfg)
	profileDir := ProfileDirForPlatform(cfg.UserDataDir, spec.Platform)
	cfg.UserDataDir = profileDir
	target, err := url.Parse(spec.IndexURL)
	if err != nil {
		return err
	}

	guard, identity, err := AcquireProfileLock(profileDir)
	if err != nil {
		return err
	}
	defer guard.Close()
	profileDir = identity
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	clearedStaleLock, err := prepareInteractiveLoginProfile(profileDir, nil)
	if err != nil {
		if errors.Is(err, errInteractiveLoginInProgress) {
			return fmt.Errorf("%s 的 DigCrawl 登录浏览器仍在运行；为避免中断当前登录，本命令不会接管它。请回到原终端完成登录并按 Enter，或关闭该窗口后重试", spec.Platform)
		}
		return err
	}
	if clearedStaleLock {
		fmt.Fprintln(os.Stderr, "已清理上次异常退出遗留的 Chrome profile 锁。")
	}
	cookieSaved := false

	connection, err := launchInteractiveLoginBrowser(ctx, cfg, profileDir)
	if err != nil && connection == nil {
		// A second command can race the first between its preflight and Chrome
		// creating SingletonLock. Re-check once to give a non-takeover error; if
		// the owner has died in that small window, clean only the stale locks and
		// retry a fresh launch.
		clearedAfterLaunch, profileErr := prepareInteractiveLoginProfile(profileDir, nil)
		if errors.Is(profileErr, errInteractiveLoginInProgress) {
			return fmt.Errorf("%s 的 DigCrawl 登录浏览器仍在运行；为避免中断当前登录，本命令不会接管它。请回到原终端完成登录并按 Enter，或关闭该窗口后重试", spec.Platform)
		}
		if profileErr == nil && clearedAfterLaunch {
			fmt.Fprintln(os.Stderr, "已清理上次异常退出遗留的 Chrome profile 锁。")
			connection, err = launchInteractiveLoginBrowser(ctx, cfg, profileDir)
		}
	}
	if err != nil {
		return finishInteractiveLogin(fmt.Errorf("启动或连接浏览器失败: %w", err), false, connection)
	}
	defer func() { resultErr = finishInteractiveLogin(resultErr, cookieSaved, connection) }()

	pageCtx, pageCancel := browserOperationContext(ctx, 5*time.Second)
	page, err := createStealthPage(pageCtx, connection.browser)
	pageCancel()
	if err != nil {
		return fmt.Errorf("创建页面失败: %w", err)
	}
	page = page.Context(ctx)
	if err := page.Navigate(spec.IndexURL); err != nil {
		return fmt.Errorf("打开登录页失败: %w", err)
	}
	_ = page.WaitLoad()

	fmt.Printf("已打开 DigCrawl 独立登录浏览器：%s\n", spec.IndexURL)
	fmt.Printf("登录成功后回到终端按 Enter；Cookie 将保存到 %s\n", cfg.CookieStoreDir)
	if err := waitForLoginConfirmation(os.Stdin); err != nil {
		return err
	}
	warnings, err := persistConfirmedLoginCookies(
		func() error {
			// A completed interactive login is the sole writer of the canonical
			// session. Background browser work only reads it and therefore cannot
			// overwrite this freshly authenticated session.
			return saveDomainCookies(page, spec.CookieDomain, cfg.CookieStoreDir, target)
		},
		spec.IndexURL,
		spec.CookieURLs,
		func(rawURL string) error {
			if err := page.Navigate(rawURL); err != nil {
				return err
			}
			return page.WaitLoad()
		},
	)
	if err != nil {
		return fmt.Errorf("保存 cookie 失败: %w", err)
	}
	cookieSaved = true
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "登录 Cookie 已保存；%v\n", warning)
	}
	fmt.Printf("已保存 %s Cookie 到 %s，正在关闭专用浏览器。\n", spec.Platform, cfg.CookieStoreDir)
	return nil
}

// launchInteractiveLoginBrowser always launches a fresh, profile-owned Chrome.
// It intentionally ignores background CDP configuration: a fixed port may
// refer to an unrelated local browser, which a login command must never close.
func launchInteractiveLoginBrowser(ctx context.Context, cfg config.LocalBrowserConfig, profileDir string) (*browserConnection, error) {
	setupCtx, cancel := browserOperationContext(ctx, 10*time.Second)
	defer cancel()
	l := launcher.New().
		Context(setupCtx).
		Headless(false).
		Set(flags.Flag("disable-gpu")).
		UserDataDir(profileDir)
	if cfg.ChromePath != "" {
		l = l.Bin(cfg.ChromePath)
	}
	l = applyInteractiveLauncherPolicy(l, cfg)

	controlURL, err := launchBrowser(l)
	connection := newBrowserConnection(nil, nil, l, true, profileDir, 0)
	if err != nil {
		return connection, err
	}
	b, transport, err := connectCDPBrowser(setupCtx, controlURL, time.Duration(cfg.TotalTimeoutSec)*time.Second)
	if err != nil {
		return connection, err
	}
	connection = newBrowserConnection(b, transport, l, true, profileDir, 0)
	return connection, nil
}

// requireInteractiveTerminal rejects detached invocations before a browser is
// launched. browser login needs stdin after the user finishes authentication to
// persist the session, so starting it from nohup, launchd, or a closed pipe
// would otherwise flash a window and then discard the login state.
func requireInteractiveTerminal(in *os.File) error {
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return fmt.Errorf("browser login 需要可交互终端以确认并保存 Cookie；请直接在终端运行，勿通过 nohup、launchd 或重定向标准输入启动")
	}
	return nil
}

// persistConfirmedLoginCookies commits the authenticated session before
// optional cross-origin warmup. A transient navigation failure must not turn a
// completed login into a lost session: the first save is authoritative, while
// later successful warmups merely refresh that snapshot with any extra cookies.
func persistConfirmedLoginCookies(save func() error, indexURL string, cookieURLs []string, navigate func(string) error) ([]error, error) {
	if err := save(); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(cookieURLs)+1)
	if parsed, err := url.Parse(indexURL); err == nil {
		seen[parsed.String()] = struct{}{}
	}
	var warnings []error
	refreshed := false
	for _, rawURL := range cookieURLs {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		urlKey := parsed.String()
		if _, ok := seen[urlKey]; ok {
			continue
		}
		seen[urlKey] = struct{}{}
		if err := navigate(rawURL); err != nil {
			warnings = append(warnings, fmt.Errorf("同步 Cookie 来源 %s 失败: %w", rawURL, err))
			continue
		}
		refreshed = true
	}
	if refreshed {
		if err := save(); err != nil {
			warnings = append(warnings, fmt.Errorf("同步后的 Cookie 刷新失败: %w", err))
		}
	}
	return warnings, nil
}

func waitForLoginConfirmation(in io.Reader) error {
	if _, err := bufio.NewReader(in).ReadString('\n'); err != nil {
		return fmt.Errorf("未收到登录确认；请在可交互终端中按 Enter，或手动运行登录命令: %w", err)
	}
	return nil
}

// CheckSession 打开平台首页并检测是否已有可用 Cookie。
func CheckSession(ctx context.Context, platform string, cfg config.LocalBrowserConfig) error {
	spec, err := LoginSpecFor(platform)
	if err != nil {
		return err
	}
	resp, err := BrowserBrowse(ctx, &BrowserBrowseRequest{
		URL:             spec.IndexURL,
		Action:          "read",
		Mode:            "summary",
		UseCookieDomain: spec.CookieDomain,
	})
	if err != nil {
		return err
	}
	if resp.Metadata != nil {
		if code := strings.TrimSpace(fmt.Sprint(resp.Metadata["error_code"])); code == "LOGIN_REQUIRED" {
			return fmt.Errorf("%s 会话未登录，请运行 digcrawl browser login --platform %s", spec.Platform, spec.Platform)
		}
	}
	fmt.Printf("%s 会话检查通过: %s\n", spec.Platform, resp.URL)
	return nil
}
