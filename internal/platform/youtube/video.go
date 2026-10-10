package youtube

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

// ReadVideo reads one public YouTube video. It uses browser-derived page state
// and optional public caption tracks. Explicit video download is owned by the
// provider's yt-dlp integration after this page read succeeds.
func ReadVideo(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	ctx = browser.WithBrowseRetryContext(ctx)
	norm, err := NormalizeURL(in.URL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind != ContentType || norm.PlatformID == "" {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "current URL is not a YouTube video", PlatformName, map[string]any{"page_kind": norm.ContentKind})
	}
	start := time.Now()
	spec := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    strings.TrimSpace(in.UseCookieDomain),
		MetadataScript:  MetadataScript(),
		CaptureMedia:    false,
	}
	var resp *browser.BrowserBrowseResponse
	err = platform.WithBrowseRetry(ctx, platform.BrowseRetryPolicy(), func(ctx context.Context) error {
		result, browseErr := platform.Browse(ctx, norm.EffectiveBrowseURL(), in, spec)
		if browseErr != nil {
			return browseErr
		}
		if validationErr := validateVideoResponse(norm, result); validationErr != nil {
			return validationErr
		}
		resp = result
		return nil
	})
	if err != nil {
		return nil, err
	}
	content := normalizeVideo(ctx, in, norm, resp)
	if strings.TrimSpace(content.Title) == "" && strings.TrimSpace(content.Text) == "" {
		return nil, platform.NewReadError(platform.ErrPartialContent, "YouTube video metadata was not extracted", PlatformName, content.PlatformMetadata)
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform: PlatformName, Channel: "browser", ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"), BrowserLatencyMs: time.Since(start).Milliseconds(),
	})
	return content, nil
}

// validateVideoResponse keeps the browser page state as the source of truth for
// the resolved resource. Missing page state is retryable because YouTube can
// render the watch shell before its player response is initialized.
func validateVideoResponse(norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) error {
	if code := platform.MetaErrorCode(resp.Metadata); platform.IsFatalErrorCode(code) {
		return platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}
	extractedID := platform.MetaString(resp.Metadata, "video_id")
	if extractedID == "" {
		return platform.NewReadError(platform.ErrPartialContent, "YouTube player state was not initialized", PlatformName, resp.Metadata)
	}
	if extractedID != norm.PlatformID {
		return platform.NewReadError(platform.ErrContentNotFound, "YouTube resolved to a different video", PlatformName, resp.Metadata)
	}
	return nil
}

func normalizeVideo(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	meta := platform.EnsureMeta(resp.Metadata)
	title := firstNonEmpty(platform.MetaString(meta, "title"), resp.Title)
	description := firstNonEmpty(platform.MetaString(meta, "description"), resp.Text)
	text := description
	tracks := captionTracksFromMeta(meta)
	if len(tracks) > 0 {
		meta["captions_available"] = true
	}
	delete(meta, "caption_tracks")
	delete(meta, "caption_text")
	delete(meta, "caption_source")
	if captions, track, err := fetchFirstCaption(ctx, tracks); err == nil {
		text = joinText(description, captions)
		meta["caption_language"] = track.LanguageCode
		meta["caption_name"] = track.Name
		meta["caption_kind"] = track.Kind
		meta["caption_source"] = "youtube_timedtext"
	} else if len(tracks) > 0 {
		meta = platform.AppendWarning(meta, "YouTube captions were advertised but could not be read")
	}
	markdown := text
	if title != "" && !strings.Contains(markdown, title) {
		markdown = fmt.Sprintf("# %s\n\n%s", title, markdown)
	}
	content := &platform.Content{
		Platform: PlatformName, ContentType: ContentType, SourceURL: norm.OriginalURL, CanonicalURL: norm.CanonicalURL,
		Title: title, Text: text, Markdown: strings.TrimSpace(markdown), Author: platform.AuthorFromMeta(meta),
		PublishedAt: platform.MetaString(meta, "published_at"), CapturedAt: platform.NowRFC3339(), PlatformMetadata: meta,
	}
	content.PlatformMetadata["platform_id"] = norm.PlatformID
	content.PlatformMetadata["video_id"] = norm.PlatformID
	content.PlatformMetadata["page_kind"] = ContentType
	if in.IncludeMedia {
		// The player response can attach format-negotiation parameters to the
		// thumbnail URL. Those parameters may force AVIF even when the downstream
		// OCR provider cannot consume it. The stable public thumbnail endpoint is
		// equivalent for this video and lets the existing image download path
		// negotiate a broadly supported format.
		if platform.MetaString(meta, "cover_url") != "" {
			content.Media = []platform.MediaItem{{
				Type: "image",
				URL:  fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", norm.PlatformID),
			}}
		}
	}
	if in.IncludeScreenshot {
		content.ScreenshotBase64 = resp.ScreenshotBase
	}
	return content
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func joinText(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, "\n\n")
}
