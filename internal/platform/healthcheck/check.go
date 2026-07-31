package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/douyin"
	"github.com/originaleric/digcrawl/internal/platform/instagram"
	"github.com/originaleric/digcrawl/internal/platform/threads"
	"github.com/originaleric/digcrawl/internal/platform/toutiao"
	"github.com/originaleric/digcrawl/internal/platform/xiaohongshu"
)

// Options 健康检查参数。
type Options struct {
	Platform  string
	ProbeURL  string
	TrialRead bool
	Timeout   time.Duration
}

// Check 执行 pong（首页）+ 可选试读。
func Check(ctx context.Context, opts Options) error {
	platformName := strings.ToLower(strings.TrimSpace(opts.Platform))
	spec, err := browser.LoginSpecFor(platformName)
	if err != nil {
		return err
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	resp, err := browser.BrowserBrowse(ctx, &browser.BrowserBrowseRequest{
		URL:             spec.IndexURL,
		Action:          "read",
		Mode:            "summary",
		UseCookieDomain: spec.CookieDomain,
	})
	if err != nil {
		return fmt.Errorf("pong 失败: %w", err)
	}
	fmt.Printf("[check] pong ok platform=%s url=%s\n", spec.Platform, resp.URL)

	probeURL := strings.TrimSpace(opts.ProbeURL)
	if probeURL == "" {
		probeURL = defaultProbeURL(platformName)
	}
	if probeURL == "" {
		if opts.TrialRead {
			return fmt.Errorf("试读需要 --url 或配置 Platform.HealthProbes.%s", platformName)
		}
		return nil
	}

	fmt.Printf("[check] trial read platform=%s url=%s\n", platformName, probeURL)
	in := platform.ReadInput{URL: probeURL, IncludeMedia: false}
	var content *platform.Content
	switch platformName {
	case "xiaohongshu":
		content, err = xiaohongshu.ReadNote(ctx, in)
	case "douyin":
		content, err = douyin.ReadVideo(ctx, in)
	case "toutiao":
		content, err = toutiao.ReadArticle(ctx, in)
	case "threads":
		content, err = threads.ReadPost(ctx, in)
	case "instagram":
		content, err = instagram.ReadContent(ctx, in)
	default:
		return fmt.Errorf("不支持试读平台 %q", platformName)
	}
	if err != nil {
		return fmt.Errorf("试读失败: %w", err)
	}
	src := platform.MetaString(content.PlatformMetadata, "extraction_source")
	fmt.Printf("[check] trial ok title=%q source=%s channel=%s\n",
		content.Title,
		src,
		platform.MetaString(content.PlatformMetadata, "read_channel"),
	)
	return nil
}

func defaultProbeURL(platform string) string {
	cfg := config.Get()
	if cfg == nil || cfg.Platform.HealthProbes == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Platform.HealthProbes[platform])
}
