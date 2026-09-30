package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var errInteractiveLoginInProgress = errors.New("interactive login browser is already using this profile")

// prepareInteractiveLoginProfile uses Chromium's own SingletonLock as the
// single source of truth for a platform login. A live owner is never touched;
// only a lock whose local owner PID is conclusively gone is removed.
func prepareInteractiveLoginProfile(profileDir string, processAlive func(int) bool) (bool, error) {
	if strings.TrimSpace(profileDir) == "" {
		return false, fmt.Errorf("interactive login profile is empty")
	}
	if processAlive == nil {
		processAlive = interactiveLoginProcessAlive
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return false, fmt.Errorf("创建登录 profile 目录失败: %w", err)
	}

	target, err := os.Readlink(filepath.Join(profileDir, "SingletonLock"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("读取 Chrome profile 锁失败: %w", err)
	}
	pid, ok := chromeSingletonLockPID(target)
	if !ok {
		return false, fmt.Errorf("无法确认 Chrome profile 锁的本机所有者")
	}
	if processAlive(pid) {
		return false, errInteractiveLoginInProgress
	}
	if err := removeChromeSingletonLocks(profileDir); err != nil {
		return false, err
	}
	return true, nil
}

// chromeSingletonLockPID accepts Chrome's local SingletonLock target format:
// <hostname>-<pid>. Hosts are checked before a PID can authorize deletion, so
// a profile shared from another machine is never treated as stale locally.
func chromeSingletonLockPID(target string) (int, bool) {
	target = strings.TrimSpace(target)
	separator := strings.LastIndex(target, "-")
	if separator <= 0 || separator == len(target)-1 {
		return 0, false
	}
	lockHost := normalizeChromeLockHost(target[:separator])
	localHost, err := os.Hostname()
	if err != nil || lockHost == "" || lockHost != normalizeChromeLockHost(localHost) {
		return 0, false
	}
	pid, err := strconv.Atoi(target[separator+1:])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func normalizeChromeLockHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.TrimSuffix(host, ".local")
}

func removeChromeSingletonLocks(profileDir string) error {
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		path := filepath.Join(profileDir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("移除失效 Chrome profile 锁 %s 失败: %w", name, err)
		}
	}
	return nil
}

func interactiveLoginProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func finishInteractiveLogin(result error, cookieSaved bool, connection *browserConnection) error {
	return finishInteractiveLoginCleanup(result, cookieSaved, cleanupInteractiveBrowser(connection))
}

func finishInteractiveLoginCleanup(result error, cookieSaved bool, cleanupErr error) error {
	if cleanupErr == nil {
		return result
	}
	if cookieSaved {
		cleanupErr = fmt.Errorf("Cookie 已保存，但专用浏览器或登录 profile 未完全释放: %w", cleanupErr)
	} else {
		cleanupErr = fmt.Errorf("登录失败后专用浏览器或登录 profile 未完全释放: %w", cleanupErr)
	}
	if result == nil {
		return cleanupErr
	}
	return errors.Join(result, cleanupErr)
}

// cleanupInteractiveBrowser reuses the owned-browser lifecycle for a visible
// login browser and reports, rather than hides, any release failure.
func cleanupInteractiveBrowser(connection *browserConnection) error {
	ctx, cancel := context.WithTimeout(context.Background(), browserShutdownTimeout)
	defer cancel()
	return connection.closeResources(ctx)
}
