package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/originaleric/digcrawl/config"
)

// manualYouTubeLoginArgs deliberately describes an ordinary Chrome launch.
// No debugging endpoint, automation flags, injected scripts, daily profile,
// or crawler ExtraArgs are part of the user-operated authentication phase.
func manualYouTubeLoginArgs(profileDir string) []string {
	return []string{"--user-data-dir=" + profileDir, "--no-first-run", "--no-default-browser-check", "https://www.youtube.com"}
}

type manualLoginChrome struct {
	cmd      *exec.Cmd
	done     chan struct{}
	identity string
}

func startManualLoginChrome(ctx context.Context, chromePath, profileDir string) (*manualLoginChrome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(chromePath, manualYouTubeLoginArgs(profileDir)...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 YouTube 专用 Chrome 失败: %w", err)
	}
	chrome := &manualLoginChrome{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(chrome.done) }()
	identityCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	chrome.identity, _ = ownedProcessIdentity(identityCtx, cmd.Process.Pid)
	cancel()
	select {
	case <-chrome.done:
		return nil, fmt.Errorf("YouTube 专用 Chrome 已退出，未进入手动登录阶段；请检查 Chrome 或 profile 占用")
	default:
	}
	return chrome, nil
}

func (chrome *manualLoginChrome) close(ctx context.Context) error {
	select {
	case <-chrome.done:
		return nil
	default:
	}
	identity, err := ownedProcessIdentity(ctx, chrome.cmd.Process.Pid)
	if err != nil {
		return err
	}
	if identity != "" {
		if chrome.identity == "" || identity != chrome.identity {
			return fmt.Errorf("无法确认 YouTube 专用 Chrome 的进程所有者；请手动关闭该窗口")
		}
		// SIGINT requests normal Chrome shutdown on Unix. Windows users close
		// this dedicated window themselves; no force-kill can discard cookies.
		if err := chrome.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("请手动关闭 YouTube 专用 Chrome 后重试: %w", err)
		}
	}
	select {
	case <-chrome.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("YouTube 专用 Chrome 尚未退出；请手动关闭该窗口: %w", ctx.Err())
	}
}

func confirmManualYouTubeLogin(ctx context.Context, cfg config.LocalBrowserConfig, profileDir string, in io.Reader) (resultErr error) {
	chrome, err := startManualLoginChrome(ctx, cfg.ChromePath, profileDir)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), browserShutdownTimeout)
		defer cancel()
		resultErr = errors.Join(resultErr, chrome.close(closeCtx))
	}()
	fmt.Println("已打开 YouTube 专用 Chrome：登录阶段不建立自动化连接。")
	fmt.Println("请点击登录，由你完成 Google 登录并返回 YouTube；完成后回到终端按 Enter。")
	if runtime.GOOS == "windows" {
		fmt.Println("Windows：请先关闭这个专用 Chrome 的全部窗口，再按 Enter。")
	}
	if err := waitForLoginConfirmationContext(ctx, in); err != nil {
		return err
	}
	closeCtx, cancel := context.WithTimeout(ctx, browserShutdownTimeout)
	defer cancel()
	if err := chrome.close(closeCtx); err != nil {
		return err
	}
	if err := waitForBrowserResourceRelease(closeCtx, profileDir, 0); err != nil {
		return fmt.Errorf("YouTube 登录 profile 尚未释放，未同步会话: %w", err)
	}
	fmt.Println("手动登录阶段已结束，正在同步 YouTube 本机会话。")
	return nil
}
