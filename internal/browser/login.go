package browser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/stealth"
	"github.com/originaleric/digcrawl/config"
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
func InteractiveLogin(ctx context.Context, platform string, cfg config.LocalBrowserConfig) error {
	spec, err := LoginSpecFor(platform)
	if err != nil {
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

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	l := launcher.New().
		Context(ctx).
		NoSandbox(true).
		Headless(false).
		Set(flags.Flag("disable-gpu")).
		UserDataDir(profileDir)
	if cfg.ChromePath != "" {
		l = l.Bin(cfg.ChromePath)
	}
	if cfg.CDPMode != nil && *cfg.CDPMode {
		port := cfg.CDPPort
		if port <= 0 {
			port = 9222
		}
		l = l.RemoteDebuggingPort(port)
	}
	l = applyLauncherExtraArgs(l, cfg.ExtraArgs)

	controlURL, err := l.Launch()
	if err != nil {
		return fmt.Errorf("启动浏览器失败: %w", err)
	}
	b := rod.New().ControlURL(controlURL).Context(ctx).Timeout(time.Duration(cfg.TotalTimeoutSec) * time.Second)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("连接浏览器失败: %w", err)
	}
	defer b.Close()

	page, err := stealth.Page(b)
	if err != nil {
		return fmt.Errorf("创建页面失败: %w", err)
	}
	if err := page.Navigate(spec.IndexURL); err != nil {
		return fmt.Errorf("打开登录页失败: %w", err)
	}
	_ = page.WaitLoad()

	fmt.Printf("已打开 DigCrawl 独立登录浏览器：%s\n", spec.IndexURL)
	fmt.Printf("登录成功后回到终端按 Enter；Cookie 将保存到 %s\n", cfg.CookieStoreDir)
	if err := waitForLoginConfirmation(os.Stdin); err != nil {
		return err
	}

	for _, raw := range spec.CookieURLs {
		_, parseErr := url.Parse(raw)
		if parseErr != nil {
			continue
		}
		if err := page.Navigate(raw); err != nil {
			return fmt.Errorf("打开 %s 以同步 cookie 失败: %w", raw, err)
		}
		_ = page.WaitLoad()
	}
	// A completed interactive login is the sole writer of the canonical session.
	// Background browser work only reads it and therefore cannot overwrite this
	// freshly authenticated session.
	if err := saveDomainCookies(page, spec.CookieDomain, cfg.CookieStoreDir, target); err != nil {
		return fmt.Errorf("保存 cookie 失败: %w", err)
	}
	fmt.Printf("已保存 %s Cookie 到 %s\n", spec.Platform, cfg.CookieStoreDir)
	return nil
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
