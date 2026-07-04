package x

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/originaleric/digcrawl/internal/platform"
)

// MapAPIResponse maps the X tweet lookup response to the common content model.
func MapAPIResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed *tweetLookupResponse) (*platform.Content, error) {
	if parsed == nil || strings.TrimSpace(parsed.Data.ID) == "" {
		return nil, fmt.Errorf("api response missing tweet payload")
	}

	tweet := parsed.Data
	author := authorForTweet(parsed, tweet.AuthorID)
	canonical := canonicalPostURL(norm, tweet.ID, author)
	text := strings.TrimSpace(tweet.Text)
	title := titleFromTweetText(text)

	meta := map[string]any{
		"extraction_source":      "api",
		"platform_id":            tweet.ID,
		"conversation_id":        strings.TrimSpace(tweet.ConversationID),
		"lang":                   strings.TrimSpace(tweet.Lang),
		"possibly_sensitive":     tweet.PossiblySensitive,
		"referenced_tweet_count": len(tweet.ReferencedTweets),
	}
	if norm != nil && len(norm.RiskFlags) > 0 {
		meta["risk_flags"] = norm.RiskFlags
	}
	if author.Username != "" {
		meta["author_username"] = author.Username
	}
	if author.Verified {
		meta["author_verified"] = true
	}

	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        sourceURL(in, norm),
		CanonicalURL:     canonical,
		Title:            title,
		Text:             text,
		Markdown:         text,
		PublishedAt:      strings.TrimSpace(tweet.CreatedAt),
		CapturedAt:       platform.NowRFC3339(),
		Engagement:       engagementFromTweet(tweet.PublicMetrics),
		PlatformMetadata: meta,
	}
	if author.ID != "" || author.Name != "" || author.Username != "" {
		content.Author = &platform.Author{
			ID:         author.ID,
			Name:       firstNonEmpty(author.Name, author.Username),
			ProfileURL: profileURL(author.Username),
			AvatarURL:  author.ProfileImageURL,
		}
	}
	if in.IncludeMedia {
		content.Media = mediaFromTweet(parsed, tweet.Attachments.MediaKeys)
	}
	return content, nil
}

func sourceURL(in platform.ReadInput, norm *platform.NormalizedURL) string {
	if norm != nil && strings.TrimSpace(norm.OriginalURL) != "" {
		return norm.OriginalURL
	}
	return strings.TrimSpace(in.URL)
}

func canonicalPostURL(norm *platform.NormalizedURL, id string, author tweetUser) string {
	if author.Username != "" && id != "" {
		return fmt.Sprintf("https://x.com/%s/status/%s", author.Username, id)
	}
	if norm != nil && strings.TrimSpace(norm.CanonicalURL) != "" {
		return norm.CanonicalURL
	}
	if id != "" {
		return fmt.Sprintf("https://x.com/i/status/%s", id)
	}
	return ""
}

func authorForTweet(parsed *tweetLookupResponse, authorID string) tweetUser {
	for _, u := range parsed.Includes.Users {
		if strings.TrimSpace(u.ID) == strings.TrimSpace(authorID) {
			return u
		}
	}
	if len(parsed.Includes.Users) > 0 {
		return parsed.Includes.Users[0]
	}
	return tweetUser{ID: authorID}
}

func mediaFromTweet(parsed *tweetLookupResponse, keys []string) []platform.MediaItem {
	if parsed == nil || len(keys) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	var out []platform.MediaItem
	for _, media := range parsed.Includes.Media {
		if _, ok := allowed[strings.TrimSpace(media.MediaKey)]; !ok {
			continue
		}
		u := strings.TrimSpace(media.URL)
		if u == "" {
			u = strings.TrimSpace(media.PreviewImageURL)
		}
		if u == "" {
			continue
		}
		out = append(out, platform.MediaItem{
			Type: strings.TrimSpace(media.Type),
			URL:  u,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func engagementFromTweet(m tweetPublicMetrics) *platform.Engagement {
	e := &platform.Engagement{
		Likes:     m.LikeCount,
		Comments:  m.ReplyCount,
		Shares:    m.QuoteCount,
		Reposts:   m.RetweetCount,
		Bookmarks: m.BookmarkCount,
	}
	if e.Likes == 0 && e.Comments == 0 && e.Shares == 0 && e.Reposts == 0 && e.Bookmarks == 0 {
		return nil
	}
	return e
}

func titleFromTweetText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	const maxRunes = 80
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func profileURL(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return "https://x.com/" + username
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
