package reddit

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

type RedditComment struct {
	ID         string          `json:"id,omitempty"`
	Fullname   string          `json:"fullname,omitempty"`
	ParentID   string          `json:"parent_id,omitempty"`
	Author     string          `json:"author,omitempty"`
	Body       string          `json:"body,omitempty"`
	CreatedUTC int64           `json:"created_utc,omitempty"`
	Score      int             `json:"score,omitempty"`
	Depth      int             `json:"depth,omitempty"`
	Replies    []RedditComment `json:"replies,omitempty"`
}

func mapPost(in ReadInput, norm *platform.NormalizedURL, post *redditThing, commentThings []redditThing, opts PostReadOptions) (*platform.Content, error) {
	if post == nil || strings.TrimSpace(post.Data.ID) == "" {
		return nil, fmt.Errorf("reddit post response missing post")
	}
	data := post.Data
	text := strings.TrimSpace(html.UnescapeString(data.SelfText))
	if text == "" && strings.TrimSpace(data.URL) != "" && !data.IsSelf {
		text = strings.TrimSpace(data.URL)
	}
	var comments []RedditComment
	var more []map[string]any
	if opts.IncludeComments && opts.CommentLimit > 0 {
		comments, more = parseComments(commentThings, opts.CommentDepth, opts.CommentLimit)
	}
	meta := map[string]any{
		"extraction_source": "api",
		"platform_id":       data.ID,
		"fullname":          firstNonEmpty(data.Name, "t3_"+data.ID),
		"subreddit":         strings.TrimSpace(data.Subreddit),
		"score":             data.Score,
		"ups":               data.Ups,
		"downs":             data.Downs,
		"upvote_ratio":      data.UpvoteRatio,
		"over_18":           data.Over18,
		"spoiler":           data.Spoiler,
		"stickied":          data.Stickied,
		"locked":            data.Locked,
		"link_flair_text":   strings.TrimSpace(data.LinkFlairText),
		"reddit": map[string]any{
			"score":        data.Score,
			"upvote_ratio": data.UpvoteRatio,
			"subreddit":    strings.TrimSpace(data.Subreddit),
		},
	}
	if len(comments) > 0 {
		meta["comments_tree"] = comments
	}
	if len(more) > 0 {
		meta["more_comments"] = more
		meta["comments_truncated"] = true
	}
	canonical := "https://www.reddit.com" + strings.TrimSpace(data.Permalink)
	if strings.TrimSpace(data.Permalink) == "" {
		canonical = norm.CanonicalURL
	}
	title := strings.TrimSpace(html.UnescapeString(data.Title))
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        firstNonEmpty(in.URL, norm.OriginalURL),
		CanonicalURL:     canonical,
		Title:            title,
		Text:             text,
		Markdown:         markdownForPost(title, text, comments, &data),
		PublishedAt:      timeFromUTC(data.CreatedUTC),
		CapturedAt:       platform.NowRFC3339(),
		Engagement:       &platform.Engagement{Comments: data.NumComments},
		PlatformMetadata: meta,
	}
	if data.Author != "" {
		content.Author = &platform.Author{Name: data.Author, ProfileURL: "https://www.reddit.com/user/" + data.Author + "/"}
	}
	if in.IncludeMedia {
		content.Media = mediaFromPost(data)
	}
	return content, nil
}

func parseComments(children []redditThing, maxDepth, limit int) ([]RedditComment, []map[string]any) {
	if maxDepth < 0 {
		maxDepth = 0
	}
	var out []RedditComment
	var more []map[string]any
	count := 0
	var walk func([]redditThing, int) []RedditComment
	walk = func(items []redditThing, depth int) []RedditComment {
		if depth > maxDepth || count >= limit {
			return nil
		}
		var comments []RedditComment
		for _, item := range items {
			if count >= limit {
				break
			}
			switch item.Kind {
			case "more":
				more = append(more, map[string]any{"count": item.Data.Count, "children": item.Data.Children, "parent_id": item.Data.ParentID})
				continue
			case "t1":
			default:
				continue
			}
			body := strings.TrimSpace(html.UnescapeString(item.Data.Body))
			if body == "" {
				continue
			}
			count++
			c := RedditComment{
				ID:         item.Data.ID,
				Fullname:   firstNonEmpty(item.Data.Name, "t1_"+item.Data.ID),
				ParentID:   item.Data.ParentID,
				Author:     item.Data.Author,
				Body:       body,
				CreatedUTC: int64(item.Data.CreatedUTC),
				Score:      item.Data.Score,
				Depth:      depth,
			}
			if len(item.Data.Replies) > 0 && string(item.Data.Replies) != `""` {
				var listing redditListing
				if err := json.Unmarshal(item.Data.Replies, &listing); err == nil {
					c.Replies = walk(listing.Data.Children, depth+1)
				}
			}
			comments = append(comments, c)
		}
		return comments
	}
	out = walk(children, 0)
	return out, more
}

func listingItemFromThing(t redditThing) ListingItem {
	d := t.Data
	canonical := "https://www.reddit.com" + strings.TrimSpace(d.Permalink)
	if strings.TrimSpace(d.Permalink) == "" {
		canonical = strings.TrimSpace(d.URL)
	}
	id := strings.TrimSpace(d.ID)
	prefix := t.Kind
	if prefix == "" {
		prefix = "t3"
	}
	fullname := firstNonEmpty(d.Name, prefix+"_"+id)
	title := strings.TrimSpace(html.UnescapeString(d.Title))
	body := strings.TrimSpace(html.UnescapeString(d.Body))
	if title == "" && body != "" {
		title = body
		if len([]rune(title)) > 120 {
			title = string([]rune(title)[:120]) + "..."
		}
	}
	return ListingItem{
		ID:           id,
		Kind:         t.Kind,
		Fullname:     fullname,
		Subreddit:    strings.TrimSpace(d.Subreddit),
		Author:       strings.TrimSpace(d.Author),
		Title:        title,
		Body:         body,
		URL:          strings.TrimSpace(d.URL),
		Permalink:    strings.TrimSpace(d.Permalink),
		CreatedUTC:   int64(d.CreatedUTC),
		Score:        d.Score,
		NumComments:  d.NumComments,
		Over18:       d.Over18,
		Stickied:     d.Stickied,
		ContentKey:   "reddit:" + fullname,
		CanonicalURL: canonical,
	}
}

func mediaFromPost(d redditThingData) []platform.MediaItem {
	var out []platform.MediaItem
	for _, img := range d.Preview.Images {
		u := strings.TrimSpace(html.UnescapeString(img.Source.URL))
		if u != "" {
			out = append(out, platform.MediaItem{Type: "image", URL: u})
		}
	}
	if len(out) == 0 && strings.TrimSpace(d.Thumbnail) != "" && strings.HasPrefix(d.Thumbnail, "http") {
		out = append(out, platform.MediaItem{Type: "image", URL: strings.TrimSpace(d.Thumbnail)})
	}
	return out
}

func markdownForPost(title, text string, comments []RedditComment, data *redditThingData) string {
	var b strings.Builder
	if strings.TrimSpace(title) != "" {
		b.WriteString("# ")
		b.WriteString(strings.TrimSpace(title))
		b.WriteString("\n\n")
	}
	if data != nil {
		if data.Subreddit != "" {
			b.WriteString("- Subreddit: r/")
			b.WriteString(data.Subreddit)
			b.WriteString("\n")
		}
		if data.Author != "" {
			b.WriteString("- Author: u/")
			b.WriteString(data.Author)
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("- Score: %d\n- Comments: %d\n\n", data.Score, data.NumComments))
	}
	if strings.TrimSpace(text) != "" {
		b.WriteString("## Post\n\n")
		b.WriteString(strings.TrimSpace(text))
		b.WriteString("\n\n")
	}
	if len(comments) > 0 {
		b.WriteString("## Top Comments\n\n")
		writeCommentsMarkdown(&b, comments, 0, 10)
	}
	return strings.TrimSpace(b.String())
}

func writeCommentsMarkdown(b *strings.Builder, comments []RedditComment, depth, remaining int) int {
	for _, c := range comments {
		if remaining <= 0 {
			return remaining
		}
		indent := strings.Repeat("  ", depth)
		b.WriteString(indent)
		b.WriteString("- ")
		if c.Author != "" {
			b.WriteString("u/")
			b.WriteString(c.Author)
			b.WriteString(": ")
		}
		b.WriteString(strings.TrimSpace(c.Body))
		if c.Score != 0 {
			b.WriteString(fmt.Sprintf(" (score: %d)", c.Score))
		}
		b.WriteString("\n")
		remaining--
		remaining = writeCommentsMarkdown(b, c.Replies, depth+1, remaining)
	}
	return remaining
}

func timeFromUTC(v float64) string {
	if v <= 0 {
		return ""
	}
	return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
