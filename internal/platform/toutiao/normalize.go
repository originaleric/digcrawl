package toutiao

import (
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func normalizeArticle(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = resp.URL
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            resp.Title,
		Text:             resp.Text,
		Markdown:         resp.Markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: cloneMeta(resp.Metadata),
	}
	if norm != nil {
		c.PlatformMetadata = platform.EnsureMeta(c.PlatformMetadata)
		if norm.PlatformID != "" {
			c.PlatformMetadata["article_id"] = norm.PlatformID
		}
		if len(norm.RiskFlags) > 0 {
			c.PlatformMetadata["risk_flags"] = norm.RiskFlags
		}
		if norm.ResolvedURL != "" {
			c.PlatformMetadata["resolved_url"] = norm.ResolvedURL
		}
	}

	if resp.Metadata != nil {
		if t := strings.TrimSpace(platform.MetaString(resp.Metadata, "title")); t != "" {
			c.Title = t
		}
		if txt := strings.TrimSpace(platform.MetaString(resp.Metadata, "text")); txt != "" {
			c.Text = chooseLongerText(c.Text, txt)
		}
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		if pub := strings.TrimSpace(platform.MetaString(resp.Metadata, "published_at")); pub != "" {
			c.PublishedAt = pub
		}
		if in.IncludeMedia {
			c.Media = platform.MediaFromMeta(resp.Metadata)
		}
		if src := strings.TrimSpace(platform.MetaString(resp.Metadata, "extraction_source")); src != "" {
			c.PlatformMetadata["extraction_source"] = src
		}
	}
	if strings.TrimSpace(platform.MetaString(c.PlatformMetadata, "extraction_source")) == "" && strings.TrimSpace(c.Text) != "" {
		c.PlatformMetadata = platform.EnsureMeta(c.PlatformMetadata)
		c.PlatformMetadata["extraction_source"] = "dom"
	}
	if mdText := textFromMarkdown(c.Markdown); shouldPreferMarkdownText(c.Text, mdText) {
		c.Text = mdText
	}
	if c.Author == nil {
		if author := authorFromText(c.Text); author != "" {
			c.Author = &platform.Author{Name: author}
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}

var (
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\([^\)]*\)`)
	markdownLinkPattern  = regexp.MustCompile(`\[([^\]]+)\]\([^\)]*\)`)
)

func chooseLongerText(current, candidate string) string {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if current == "" {
		return candidate
	}
	if candidate == "" {
		return current
	}
	if len([]rune(candidate)) >= len([]rune(current)) {
		return candidate
	}
	return current
}

func shouldPreferMarkdownText(current, candidate string) bool {
	currentLen := len([]rune(strings.TrimSpace(current)))
	candidateLen := len([]rune(strings.TrimSpace(candidate)))
	if candidateLen < 200 {
		return false
	}
	if currentLen == 0 {
		return true
	}
	return candidateLen >= currentLen*2
}

func textFromMarkdown(markdown string) string {
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return ""
	}
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "![](") || strings.HasPrefix(line, "![") {
			continue
		}
		line = markdownImagePattern.ReplaceAllString(line, "")
		line = markdownLinkPattern.ReplaceAllString(line, "$1")
		line = strings.TrimSpace(strings.TrimLeft(line, "#>"))
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n\n")
}

func authorFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{"来源：", "来源:"} {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
		break
	}
	return ""
}

func cloneMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}
