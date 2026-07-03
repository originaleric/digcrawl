package wechat

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	htmlmd "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/originaleric/digcrawl/internal/platform"
	xhtml "golang.org/x/net/html"
)

type parsedArticle struct {
	Title             string
	TitleSource       string
	AuthorName        string
	AuthorSource      string
	PublishedAt       string
	PublishedAtSource string
	Text              string
	HTML              string
	Markdown          string
	ImageURLs         []string
	BodyTextLength    int
	Warnings          []string
}

func parseArticleHTML(rawHTML, sourceURL string) (*parsedArticle, error) {
	doc, err := xhtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	out := &parsedArticle{}
	if title := strings.TrimSpace(textOfNode(firstNodeByID(doc, "activity-name"))); title != "" {
		out.Title = normalizeSpace(title)
		out.TitleSource = "#activity-name"
	}
	if inner := firstNodeByClass(firstNodeByID(doc, "activity-name"), "js_title_inner"); inner != nil {
		if title := strings.TrimSpace(textOfNode(inner)); title != "" {
			out.Title = normalizeSpace(title)
			out.TitleSource = "#activity-name .js_title_inner"
		}
	}
	if out.Title == "" {
		if title := scriptStringValue(rawHTML, "msg_title"); title != "" {
			out.Title = normalizeSpace(title)
			out.TitleSource = "script.msg_title"
		}
	}

	if name := strings.TrimSpace(textOfNode(firstNodeByID(doc, "js_name"))); name != "" {
		out.AuthorName = normalizeSpace(name)
		out.AuthorSource = "#js_name"
	} else if name := scriptStringValue(rawHTML, "nickname"); name != "" {
		out.AuthorName = normalizeSpace(name)
		out.AuthorSource = "script.nickname"
	}

	if pub := strings.TrimSpace(textOfNode(firstNodeByID(doc, "publish_time"))); pub != "" {
		out.PublishedAt = normalizeSpace(pub)
		out.PublishedAtSource = "#publish_time"
	} else if createTime := scriptStringValue(rawHTML, "createTime"); createTime != "" {
		out.PublishedAt = normalizeSpace(createTime)
		out.PublishedAtSource = "script.createTime"
	} else if ct := scriptStringValue(rawHTML, "ct"); ct != "" {
		out.PublishedAt = unixTimestampToRFC3339(ct)
		if out.PublishedAt == "" {
			out.PublishedAt = ct
		}
		out.PublishedAtSource = "script.ct"
	}

	body := firstNodeByID(doc, "js_content")
	if body == nil {
		body = firstNodeByClass(doc, "rich_media_content")
	}
	if body == nil {
		body = firstNodeByID(doc, "js_article")
	}
	if body == nil {
		return out, fmt.Errorf("article body missing")
	}

	normalizeImages(body)
	bodyHTML, err := renderChildren(body)
	if err != nil {
		return out, fmt.Errorf("render body html: %w", err)
	}
	out.HTML = strings.TrimSpace(bodyHTML)
	out.Text = normalizeSpace(textOfNode(body))
	out.BodyTextLength = len([]rune(out.Text))
	out.ImageURLs = imageURLs(body)
	converter := htmlmd.NewConverter("", true, nil)
	if md, err := converter.ConvertString(out.HTML); err == nil {
		out.Markdown = strings.TrimSpace(md)
	} else {
		out.Warnings = append(out.Warnings, "markdown conversion failed: "+err.Error())
	}
	if out.Text == "" {
		out.Warnings = append(out.Warnings, "article body empty")
	}
	if strings.TrimSpace(out.Title) == "" {
		out.Warnings = append(out.Warnings, "article title missing")
	}
	return out, nil
}

func articleContentFromParsed(in platform.ReadInput, sourceURL, finalURL string, parsed *parsedArticle, meta map[string]any) *platform.Content {
	if meta == nil {
		meta = map[string]any{}
	}
	if finalURL == "" {
		finalURL = sourceURL
	}
	meta["title_source"] = parsed.TitleSource
	meta["author_source"] = parsed.AuthorSource
	meta["published_at_source"] = parsed.PublishedAtSource
	meta["body_text_length"] = parsed.BodyTextLength
	meta["image_count"] = len(parsed.ImageURLs)
	if _, ok := meta["warnings"]; !ok && len(parsed.Warnings) > 0 {
		meta["warnings"] = parsed.Warnings
	}
	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        finalURL,
		CanonicalURL:     finalURL,
		Title:            parsed.Title,
		Text:             parsed.Text,
		Markdown:         parsed.Markdown,
		HTML:             parsed.HTML,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if parsed.AuthorName != "" {
		c.Author = &platform.Author{Name: parsed.AuthorName}
	}
	if parsed.PublishedAt != "" {
		c.PublishedAt = parsed.PublishedAt
	}
	if in.IncludeMedia {
		for _, u := range parsed.ImageURLs {
			c.Media = append(c.Media, platform.MediaItem{Type: "image", URL: u})
		}
	}
	return c
}

func firstNodeByID(root *xhtml.Node, id string) *xhtml.Node {
	return firstNode(root, func(n *xhtml.Node) bool {
		return attrValue(n, "id") == id
	})
}

func firstNodeByClass(root *xhtml.Node, class string) *xhtml.Node {
	return firstNode(root, func(n *xhtml.Node) bool {
		return hasClass(attrValue(n, "class"), class)
	})
}

func firstNode(root *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if found := firstNode(c, match); found != nil {
			return found
		}
	}
	return nil
}

func attrValue(n *xhtml.Node, name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *xhtml.Node, name, value string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, xhtml.Attribute{Key: name, Val: value})
}

func hasClass(raw, want string) bool {
	for _, part := range strings.Fields(raw) {
		if part == want {
			return true
		}
	}
	return false
}

func textOfNode(n *xhtml.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			switch strings.ToLower(node.Data) {
			case "script", "style", "noscript":
				return
			case "br", "p", "section", "div", "h1", "h2", "h3", "li":
				b.WriteString("\n")
			}
		}
		if node.Type == xhtml.TextNode {
			b.WriteString(node.Data)
			b.WriteString(" ")
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if node.Type == xhtml.ElementNode {
			switch strings.ToLower(node.Data) {
			case "p", "section", "div", "h1", "h2", "h3", "li":
				b.WriteString("\n")
			}
		}
	}
	walk(n)
	return b.String()
}

func renderChildren(n *xhtml.Node) (string, error) {
	if n == nil {
		return "", nil
	}
	var b bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&b, c); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func normalizeImages(root *xhtml.Node) {
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "img") {
			if src := strings.TrimSpace(attrValue(n, "src")); src == "" {
				if dataSrc := strings.TrimSpace(attrValue(n, "data-src")); dataSrc != "" {
					setAttr(n, "src", dataSrc)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

func imageURLs(root *xhtml.Node) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "img") {
			u := strings.TrimSpace(attrValue(n, "data-src"))
			if u == "" {
				u = strings.TrimSpace(attrValue(n, "src"))
			}
			if u != "" && !strings.HasPrefix(u, "data:") && !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func normalizeSpace(s string) string {
	fields := strings.Fields(strings.ReplaceAll(html.UnescapeString(s), "\u00a0", " "))
	return strings.Join(fields, " ")
}

func scriptStringValue(rawHTML, name string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?s)var\s+` + regexp.QuoteMeta(name) + `\s*=\s*htmlDecode\("([^"]*)"\)`),
		regexp.MustCompile(`(?s)var\s+` + regexp.QuoteMeta(name) + `\s*=\s*'([^']*)'`),
		regexp.MustCompile(`(?s)var\s+` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"`),
	}
	for _, re := range patterns {
		m := re.FindStringSubmatch(rawHTML)
		if len(m) > 1 {
			return normalizeScriptValue(m[1])
		}
	}
	return ""
}

func normalizeScriptValue(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\'`, `'`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.TrimSuffix(s, ".html(false)")
	return html.UnescapeString(s)
}

func unixTimestampToRFC3339(raw string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return ""
	}
	return time.Unix(n, 0).Format(time.RFC3339)
}

func readLimited(r io.Reader, max int64) ([]byte, bool, error) {
	var b bytes.Buffer
	n, err := io.Copy(&b, io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	if n > max {
		return b.Bytes()[:max], true, nil
	}
	return b.Bytes(), false, nil
}
