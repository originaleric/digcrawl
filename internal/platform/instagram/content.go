package instagram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func ReadContent(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	ctx = browser.WithBrowseRetryContext(ctx)
	norm, err := NormalizeURL(in.URL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind != "p" && norm.ContentKind != "reel" && norm.ContentKind != "tv" {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "current URL is not an Instagram post or reel", PlatformName, map[string]any{"page_kind": norm.ContentKind})
	}
	socialCfg := config.Get().Social.Instagram
	ctx, cancel := withConfiguredTimeout(ctx, socialCfg.RequestTimeoutSec)
	defer cancel()
	if err := waitConfiguredRateLimit(ctx, socialCfg.RateLimitMs); err != nil {
		return nil, err
	}
	spec := platform.BrowseSpec{
		WaitSelector:    "article, main, body",
		ContentSelector: "article, main, body",
		CookieDomain:    strings.TrimSpace(in.UseCookieDomain),
		MetadataScript:  MetadataScript(),
		CaptureMedia:    in.IncludeMedia,
	}
	var resp *browser.BrowserBrowseResponse
	start := time.Now()
	err = platform.WithBrowseRetry(ctx, platform.BrowseRetryPolicy(), func(ctx context.Context) error {
		r, browseErr := platform.Browse(ctx, norm.EffectiveBrowseURL(), in, spec)
		if browseErr != nil {
			return browseErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	if code := platform.MetaErrorCode(resp.Metadata); platform.IsFatalErrorCode(code) {
		return nil, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}
	content := normalizeContent(in, norm, resp)
	if strings.TrimSpace(content.Text) == "" {
		return nil, platform.NewReadError(platform.ErrPartialContent, "Instagram content text was not extracted", PlatformName, content.PlatformMetadata)
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		BrowserLatencyMs: time.Since(start).Milliseconds(),
	})
	return content, nil
}

func normalizeContent(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	meta := platform.EnsureMeta(resp.Metadata)
	title := platform.MetaString(meta, "title")
	text := firstNonEmpty(platform.MetaString(meta, "description"), resp.Text)
	media := platform.MediaFromMeta(meta)
	videoURLs := platform.MetaStringSlice(meta, "video_urls")
	if resp != nil && len(resp.MediaURLs) > 0 {
		videoURLs = resp.MediaURLs
	}
	for _, u := range videoURLs {
		media = append(media, platform.MediaItem{Type: "video", URL: u})
	}
	contentType := "instagram_post"
	if norm.ContentKind == "reel" {
		contentType = "video"
	}
	markdown := strings.TrimSpace(text)
	if title != "" && !strings.Contains(markdown, title) {
		markdown = fmt.Sprintf("# %s\n\n%s", title, markdown)
	}
	return &platform.Content{
		Platform:         PlatformName,
		ContentType:      contentType,
		SourceURL:        in.URL,
		CanonicalURL:     norm.CanonicalURL,
		Title:            title,
		Text:             text,
		Markdown:         markdown,
		Author:           platform.AuthorFromMeta(meta),
		PublishedAt:      platform.MetaString(meta, "published_at"),
		CapturedAt:       platform.NowRFC3339(),
		Media:            media,
		PlatformMetadata: meta,
	}
}

func withConfiguredTimeout(ctx context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}

func waitConfiguredRateLimit(ctx context.Context, milliseconds int) error {
	if milliseconds <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
