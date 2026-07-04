package x

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

var (
	oEmbedParagraphRE = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	oEmbedAnchorRE    = regexp.MustCompile(`(?is)<a\b[^>]*href="[^"]*/status/\d+[^"]*"[^>]*>(.*?)</a>`)
	htmlTagRE         = regexp.MustCompile(`(?is)<[^>]+>`)
)

// MapOEmbedResponse maps public X oEmbed payloads to the common content model.
func MapOEmbedResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed *oEmbedResponse) (*platform.Content, error) {
	if parsed == nil || strings.TrimSpace(parsed.HTML) == "" {
		return nil, fmt.Errorf("oembed response missing html")
	}
	text := extractOEmbedText(parsed.HTML)
	if text == "" {
		return nil, fmt.Errorf("oembed response missing tweet text")
	}
	published := extractOEmbedPublished(parsed.HTML)
	canonical := strings.TrimSpace(parsed.URL)
	if canonical == "" && norm != nil {
		canonical = norm.CanonicalURL
	}

	meta := map[string]any{
		"extraction_source": "oembed",
		"platform_id":       "",
		"provider_name":     strings.TrimSpace(parsed.ProviderName),
	}
	if norm != nil {
		meta["platform_id"] = norm.PlatformID
		if len(norm.RiskFlags) > 0 {
			meta["risk_flags"] = norm.RiskFlags
		}
	}

	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        sourceURL(in, norm),
		CanonicalURL:     canonical,
		Title:            titleFromTweetText(text),
		Text:             text,
		Markdown:         text,
		HTML:             strings.TrimSpace(parsed.HTML),
		PublishedAt:      published,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if strings.TrimSpace(parsed.AuthorName) != "" || strings.TrimSpace(parsed.AuthorURL) != "" {
		content.Author = &platform.Author{
			Name:       strings.TrimSpace(parsed.AuthorName),
			ProfileURL: strings.TrimSpace(parsed.AuthorURL),
		}
	}
	return content, nil
}

func extractOEmbedText(raw string) string {
	match := oEmbedParagraphRE.FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return cleanHTMLText(match[1])
}

func extractOEmbedPublished(raw string) string {
	matches := oEmbedAnchorRE.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	return cleanHTMLText(matches[len(matches)-1][1])
}

func cleanHTMLText(raw string) string {
	raw = strings.ReplaceAll(raw, "<br>", "\n")
	raw = strings.ReplaceAll(raw, "<br/>", "\n")
	raw = strings.ReplaceAll(raw, "<br />", "\n")
	withoutTags := htmlTagRE.ReplaceAllString(raw, "")
	return strings.TrimSpace(html.UnescapeString(withoutTags))
}
