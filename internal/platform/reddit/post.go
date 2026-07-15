package reddit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

type ReadInput struct {
	platform.ReadInput
	IncludeComments bool
	CommentLimit    int
	CommentDepth    int
	CommentSort     string
	ExpandMore      bool
	ExpandMoreLimit int
}

func ReadPost(ctx context.Context, in ReadInput) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}
	norm, err := NormalizePostURL(url)
	if err != nil {
		return nil, err
	}
	cfg := config.Get().Platform.Reddit
	opts := normalizePostOptions(cfg, in)
	var apiAttempt *platform.APIAttempt
	var apiLatency int64
	if post, comments, attempt, latency, apiErr := fetchPostViaAPI(ctx, norm, cfg, opts); apiErr == nil && post != nil {
		content, err := mapPost(in, norm, post, comments, opts)
		if err == nil && content != nil {
			content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
			platform.MergeContentMetrics(content, platform.ReadMetrics{
				Platform:         PlatformName,
				Channel:          "api",
				ExtractionSource: "api",
				APILatencyMs:     latency,
			})
			return content, nil
		}
	} else if attempt != nil {
		apiAttempt = attempt
		apiLatency = latency
		if re, stop := platform.ShouldStopStrategy(apiErr); stop && !canFallbackAfterAPIError(cfg, attempt) {
			return nil, re
		}
	}
	if !cfg.BrowserFallback.EnabledOrDefault() {
		if apiAttempt != nil {
			return nil, apiAttempt.Error()
		}
		return nil, platform.NewReadError(platform.ErrPartialContent, "Reddit API failed and browser fallback is disabled", PlatformName, nil)
	}
	if apiAttempt != nil && apiAttempt.ErrorCode == platform.ErrLoginRequired && !cfg.BrowserFallback.AllowWithoutOAuthOrDefault() {
		return nil, apiAttempt.Error()
	}
	content, browserLatency, err := readPostBrowser(ctx, in, norm, apiAttempt)
	if err != nil {
		return nil, err
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		APILatencyMs:     apiLatency,
		BrowserLatencyMs: browserLatency,
	})
	return content, nil
}

func canFallbackAfterAPIError(cfg config.RedditConfig, attempt *platform.APIAttempt) bool {
	if attempt == nil {
		return false
	}
	if attempt.ErrorCode == platform.ErrLoginRequired && cfg.BrowserFallback.EnabledOrDefault() && cfg.BrowserFallback.AllowWithoutOAuthOrDefault() {
		return true
	}
	return attempt.FallbackBrowser && cfg.BrowserFallback.EnabledOrDefault()
}

func normalizePostOptions(cfg config.RedditConfig, in ReadInput) PostReadOptions {
	limit := in.CommentLimit
	if limit == 0 {
		limit = cfg.Comments.LimitOrDefault()
	}
	if limit < 0 {
		limit = 0
	}
	if limit > 500 {
		limit = 500
	}
	depth := in.CommentDepth
	if depth == 0 {
		depth = cfg.Comments.DepthOrDefault()
	}
	if depth < 0 {
		depth = 0
	}
	if depth > 10 {
		depth = 10
	}
	sort := strings.ToLower(strings.TrimSpace(in.CommentSort))
	if sort == "" {
		sort = cfg.Comments.SortOrDefault()
	}
	switch sort {
	case "confidence", "top", "new", "controversial", "old", "qa":
	default:
		sort = cfg.Comments.SortOrDefault()
	}
	expandLimit := in.ExpandMoreLimit
	if expandLimit == 0 {
		expandLimit = cfg.Comments.ExpandMoreLimit
	}
	return PostReadOptions{
		IncludeComments: in.IncludeComments,
		CommentLimit:    limit,
		CommentDepth:    depth,
		CommentSort:     sort,
		ExpandMore:      in.ExpandMore || cfg.Comments.ExpandMoreOrDefault(),
		ExpandMoreLimit: expandLimit,
	}
}

func readPostBrowser(ctx context.Context, in ReadInput, norm *platform.NormalizedURL, apiAttempt *platform.APIAttempt) (*platform.Content, int64, error) {
	base := platform.BrowseSpec{
		WaitSelector:    "shreddit-post, article, main",
		ContentSelector: "shreddit-post, article, main",
		CookieDomain:    CookieDomain,
		MetadataScript:  metadataScript,
	}
	start := time.Now()
	resp, err := platform.Browse(ctx, norm.EffectiveBrowseURL(), in.ReadInput, base)
	if err != nil {
		return nil, time.Since(start).Milliseconds(), err
	}
	meta := platform.EnsureMeta(resp.Metadata)
	if code := platform.MetaErrorCode(meta); platform.IsFatalErrorCode(code) {
		return nil, time.Since(start).Milliseconds(), platform.NewReadError(code, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	title := firstNonEmpty(platform.MetaString(meta, "title"), strings.TrimSpace(resp.Title))
	text := firstNonEmpty(platform.MetaString(meta, "selftext"), strings.TrimSpace(resp.Text))
	if title == "" || text == "" {
		meta = platform.SetMetaError(meta, platform.ErrPartialContent, "Reddit browser fallback did not extract title/text")
		return nil, time.Since(start).Milliseconds(), platform.NewReadError(platform.ErrPartialContent, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	meta["extraction_source"] = "browser"
	meta["reddit"] = map[string]any{
		"subreddit": platform.MetaString(meta, "subreddit"),
		"score":     platform.MetaString(meta, "score"),
	}
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        in.URL,
		CanonicalURL:     firstNonEmpty(platform.MetaString(meta, "permalink"), norm.CanonicalURL),
		Title:            title,
		Text:             text,
		Markdown:         markdownForPost(title, text, nil, nil),
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: platform.MergeAPIMeta(meta, apiAttempt),
	}
	if author := platform.MetaString(meta, "author"); author != "" {
		content.Author = &platform.Author{Name: author, ProfileURL: "https://www.reddit.com/user/" + author + "/"}
	}
	return content, time.Since(start).Milliseconds(), nil
}

const metadataScript = `(() => {
  const post = document.querySelector('shreddit-post');
  const pick = (sel) => (document.querySelector(sel)?.textContent || '').trim();
  const attr = (name) => post ? (post.getAttribute(name) || '') : '';
  const body = pick('[slot="text-body"], .text-neutral-content, [data-testid="post-content"]') || pick('article');
  const title = attr('post-title') || pick('h1') || document.title.replace(/\s*:\s*reddit$/i, '').trim();
  const lower = document.body ? document.body.innerText.toLowerCase() : '';
  if (lower.includes('private community') || lower.includes('this community is private')) {
    return {error_code:'CONTENT_PRIVATE', error_message:'Reddit private community'};
  }
  if (lower.includes('sign up to view') || lower.includes('log in to view')) {
    return {error_code:'LOGIN_REQUIRED', error_message:'Reddit page requires login'};
  }
  return {
    title,
    selftext: body,
    author: attr('author'),
    subreddit: attr('subreddit-prefixed-name'),
    score: attr('score'),
    permalink: location.href,
    extraction_source: 'browser'
  };
})()`
