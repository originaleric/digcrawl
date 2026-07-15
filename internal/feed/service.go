package feed

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "feed"
	ContentType  = "feed"
)

type Service struct {
	Client    *http.Client
	UserAgent string
	MaxBytes  int64
}

type Document struct {
	URL         string
	Title       string
	Description string
	Entries     []Entry
}

type Entry struct {
	ID          string
	URL         string
	Title       string
	Author      string
	AuthorURL   string
	ContentHTML string
	SummaryHTML string
	PublishedAt time.Time
	UpdatedAt   time.Time
}

func IsFeedURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return false
	}
	path := strings.ToLower(strings.TrimSpace(u.Path))
	return strings.HasSuffix(path, ".rss") ||
		strings.HasSuffix(path, ".atom") ||
		strings.HasSuffix(path, ".xml") ||
		strings.Contains(path, "/feed") ||
		strings.Contains(path, "/rss")
}

func (s Service) Read(ctx context.Context, rawURL string) (*Document, error) {
	data, _, err := s.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := Parse(data, rawURL)
	if err == nil {
		return doc, nil
	}
	if sm, smErr := ParseSitemap(data, rawURL); smErr == nil {
		return sm, nil
	}
	return nil, err
}

func (s Service) ReadSitemap(ctx context.Context, rawURL string) (*Document, error) {
	data, _, err := s.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return ParseSitemap(data, rawURL)
}

func (s Service) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	ua := strings.TrimSpace(s.UserAgent)
	if ua == "" {
		ua = "Mozilla/5.0 DigCrawl Feed"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	maxBytes := s.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", fmt.Errorf("RATE_LIMITED: feed request returned 429")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", fmt.Errorf("LOGIN_REQUIRED: feed request returned %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("feed request returned %d", resp.StatusCode)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (s Service) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func Parse(data []byte, sourceURL string) (*Document, error) {
	var rss struct {
		Title   string `xml:"title"`
		Sub     string `xml:"subtitle"`
		Channel struct {
			Title       string `xml:"title"`
			Description string `xml:"description"`
			Items       []struct {
				Title          string `xml:"title"`
				Link           string `xml:"link"`
				GUID           string `xml:"guid"`
				Date           string `xml:"pubDate"`
				Author         string `xml:"author"`
				Creator        string `xml:"http://purl.org/dc/elements/1.1/ creator"`
				Description    string `xml:"description"`
				ContentEncoded string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
			} `xml:"item"`
		} `xml:"channel"`
		Entries []struct {
			Title string `xml:"title"`
			ID    string `xml:"id"`
			Link  []struct {
				Href string `xml:"href,attr"`
				Rel  string `xml:"rel,attr"`
			} `xml:"link"`
			Author struct {
				Name string `xml:"name"`
				URI  string `xml:"uri"`
			} `xml:"author"`
			Updated   string `xml:"updated"`
			Published string `xml:"published"`
			Summary   string `xml:"summary"`
			Content   string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	doc := &Document{
		URL:         strings.TrimSpace(sourceURL),
		Title:       strings.TrimSpace(html.UnescapeString(firstNonEmpty(rss.Channel.Title, rss.Title))),
		Description: TextFromHTML(firstNonEmpty(rss.Channel.Description, rss.Sub)),
	}
	for _, it := range rss.Channel.Items {
		link := strings.TrimSpace(it.Link)
		if link == "" {
			continue
		}
		doc.Entries = append(doc.Entries, Entry{
			ID:          strings.TrimSpace(it.GUID),
			URL:         link,
			Title:       strings.TrimSpace(html.UnescapeString(it.Title)),
			Author:      strings.TrimSpace(firstNonEmpty(it.Creator, it.Author)),
			ContentHTML: strings.TrimSpace(firstNonEmpty(it.ContentEncoded, it.Description)),
			SummaryHTML: strings.TrimSpace(it.Description),
			PublishedAt: parseTime(it.Date),
		})
	}
	for _, it := range rss.Entries {
		link := atomLink(it.Link)
		if link == "" {
			continue
		}
		doc.Entries = append(doc.Entries, Entry{
			ID:          strings.TrimSpace(it.ID),
			URL:         link,
			Title:       strings.TrimSpace(html.UnescapeString(it.Title)),
			Author:      strings.TrimSpace(it.Author.Name),
			AuthorURL:   strings.TrimSpace(it.Author.URI),
			ContentHTML: strings.TrimSpace(firstNonEmpty(it.Content, it.Summary)),
			SummaryHTML: strings.TrimSpace(it.Summary),
			PublishedAt: parseTime(firstNonEmpty(it.Published, it.Updated)),
			UpdatedAt:   parseTime(it.Updated),
		})
	}
	if len(doc.Entries) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no rss items found")
	}
	if doc.Title == "" {
		doc.Title = titleFromURL(sourceURL)
	}
	return doc, nil
}

func ParseSitemap(data []byte, sourceURL string) (*Document, error) {
	var sm struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}
	doc := &Document{URL: strings.TrimSpace(sourceURL), Title: titleFromURL(sourceURL)}
	for _, u := range sm.URLs {
		link := strings.TrimSpace(u.Loc)
		if link == "" {
			continue
		}
		doc.Entries = append(doc.Entries, Entry{URL: link, ID: link, PublishedAt: parseTime(u.LastMod)})
	}
	if len(doc.Entries) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no sitemap urls found")
	}
	return doc, nil
}

func ToContent(doc *Document, sourceURL string) *platform.Content {
	if doc == nil {
		return nil
	}
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = titleFromURL(sourceURL)
	}
	entries := entriesMeta(doc.Entries)
	return &platform.Content{
		Platform:     PlatformName,
		ContentType:  ContentType,
		SourceURL:    firstNonEmpty(sourceURL, doc.URL),
		CanonicalURL: firstNonEmpty(doc.URL, sourceURL),
		Title:        title,
		Text:         documentText(doc),
		Markdown:     documentMarkdown(doc),
		CapturedAt:   platform.NowRFC3339(),
		PlatformMetadata: map[string]any{
			"extraction_source": "rss",
			"entry_count":       len(doc.Entries),
			"entries":           entries,
		},
	}
}

func entriesMeta(entries []Entry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		m := map[string]any{
			"id":    firstNonEmpty(e.ID, e.URL),
			"url":   e.URL,
			"title": e.Title,
		}
		if e.Author != "" {
			m["author"] = e.Author
		}
		if !e.PublishedAt.IsZero() {
			m["published_at"] = e.PublishedAt.Format(time.RFC3339)
		}
		if !e.UpdatedAt.IsZero() {
			m["updated_at"] = e.UpdatedAt.Format(time.RFC3339)
		}
		if summary := TextFromHTML(firstNonEmpty(e.SummaryHTML, e.ContentHTML)); summary != "" {
			m["summary"] = summary
		}
		out = append(out, m)
	}
	return out
}

func documentText(doc *Document) string {
	var b strings.Builder
	for _, e := range doc.Entries {
		if e.Title != "" {
			b.WriteString(e.Title)
			b.WriteString("\n")
		}
		if text := TextFromHTML(firstNonEmpty(e.ContentHTML, e.SummaryHTML)); text != "" {
			b.WriteString(text)
			b.WriteString("\n")
		}
		if e.URL != "" {
			b.WriteString(e.URL)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func documentMarkdown(doc *Document) string {
	var b strings.Builder
	if doc.Description != "" {
		b.WriteString(doc.Description)
		b.WriteString("\n\n")
	}
	for _, e := range doc.Entries {
		title := firstNonEmpty(e.Title, e.URL)
		b.WriteString("- ")
		if e.URL != "" {
			b.WriteString("[")
			b.WriteString(escapeMarkdownLinkText(title))
			b.WriteString("](")
			b.WriteString(e.URL)
			b.WriteString(")")
		} else {
			b.WriteString(title)
		}
		if e.Author != "" {
			b.WriteString(" by ")
			b.WriteString(e.Author)
		}
		if !e.PublishedAt.IsZero() {
			b.WriteString(" - ")
			b.WriteString(e.PublishedAt.Format(time.RFC3339))
		}
		if summary := TextFromHTML(firstNonEmpty(e.SummaryHTML, e.ContentHTML)); summary != "" {
			b.WriteString("\n  ")
			b.WriteString(summary)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func atomLink(links []struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}) string {
	fallback := ""
	for _, l := range links {
		href := strings.TrimSpace(l.Href)
		if href == "" {
			continue
		}
		rel := strings.ToLower(strings.TrimSpace(l.Rel))
		if rel == "" || rel == "alternate" {
			return href
		}
		if fallback == "" {
			fallback = href
		}
	}
	return fallback
}

var tagRe = regexp.MustCompile(`(?s)<[^>]+>`)

func TextFromHTML(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = tagRe.ReplaceAllString(v, " ")
	v = html.UnescapeString(v)
	return strings.Join(strings.Fields(v), " ")
}

func parseTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	layouts := []string{time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func titleFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return "Feed"
	}
	return u.Hostname()
}

func escapeMarkdownLinkText(v string) string {
	return strings.ReplaceAll(v, "]", "\\]")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
