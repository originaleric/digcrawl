package browser

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestFeishuDocumentAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_FEISHU_ACCEPTANCE") != "1" {
		t.Skip("remote Feishu acceptance is opt-in")
	}
	headless := true
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 60, NavigateTimeoutSec: 20, WaitSelectorTimeoutSec: 5})
	previous := *config.Get()
	config.Get().LocalBrowser = cfg
	config.Get().Gateway.AllowedDomains = nil
	defer func() { *config.Get() = previous }()
	m := &browserManager{cfg: cfg, slots: make(chan struct{}, 1), sessions: make(map[uint64]*browserSession), stopCh: make(chan struct{})}
	defer m.shutdownImmediately(context.Background())
	resp, err := browserBrowseWithSessionRunner(context.Background(), &BrowserBrowseRequest{URL: "https://flip.feishu.cn/wiki/Zz8lwC7DDiPl7dkw0hvc2Ms6nSd", Mode: "full"}, func() *browserManager { return m }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ContentCoverage["reached_end"] != true || resp.ContentCoverage["truncated"] == true {
		t.Fatalf("incomplete rendered traversal: %+v", resp.ContentCoverage)
	}
	for _, chapter := range []string{"一、功能介绍", "连接前准备", "Windows", "Macbook", "iPad", "POS连接", "蜂鸣", "打印速度"} {
		if !strings.Contains(resp.Text, chapter) {
			t.Errorf("missing body chapter %s", chapter)
		}
	}
	t.Logf("Feishu title=%s text_chars=%d coverage=%v", resp.Title, len(resp.Text), resp.ContentCoverage)
}
