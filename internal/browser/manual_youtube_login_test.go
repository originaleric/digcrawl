package browser

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestManualYouTubeLoginHasNoAutomationOrDailyProfile(t *testing.T) {
	t.Parallel()
	profile := t.TempDir()
	args := manualYouTubeLoginArgs(profile)
	if len(args) != 4 || args[0] != "--user-data-dir="+profile || args[3] != "https://www.youtube.com" {
		t.Fatalf("unexpected manual login arguments: %v", args)
	}
	for _, arg := range args {
		for _, forbidden := range []string{"debugging", "automation", "headless", "no-sandbox", "disable-web-security", "User-Agent"} {
			if strings.Contains(arg, forbidden) {
				t.Fatalf("user-operated sign-in must not use %q: %v", forbidden, args)
			}
		}
	}
}

func TestLoginConfirmationCancellationDoesNotRequireEnter(t *testing.T) {
	t.Parallel()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- waitForLoginConfirmationContext(ctx, reader) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("confirmation cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("login cancellation still waits for terminal input")
	}
}

func TestManualLoginChromeHelper(t *testing.T) {
	if os.Getenv("DIGCRAWL_MANUAL_CHROME_HELPER") != "1" {
		return
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	_, _ = os.Stdout.WriteString("ready\n")
	<-interrupt
	os.Exit(0)
}

func TestManualLoginChromeClosesOnlyVerifiedOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows manual login asks the user to close the dedicated window")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestManualLoginChromeHelper$")
	cmd.Env = append(os.Environ(), "DIGCRAWL_MANUAL_CHROME_HELPER=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	chrome := &manualLoginChrome{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(chrome.done) }()
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("helper readiness: %q %v", line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	identity, err := ownedProcessIdentity(ctx, cmd.Process.Pid)
	if err != nil || identity == "" {
		t.Fatalf("helper owner identity: %q %v", identity, err)
	}
	chrome.identity = "unrelated-process"
	if err := chrome.close(ctx); err == nil {
		t.Fatal("a process with different identity was closed")
	}
	select {
	case <-chrome.done:
		t.Fatal("unverified owner was signaled")
	default:
	}
	chrome.identity = identity
	if err := chrome.close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := chrome.close(ctx); err != nil {
		t.Fatalf("close is not idempotent: %v", err)
	}
}
