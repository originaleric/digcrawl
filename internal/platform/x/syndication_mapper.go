package x

import (
	"fmt"
	"sort"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

type syndicationTweetResponse struct {
	IDStr             string                    `json:"id_str"`
	Text              string                    `json:"text"`
	CreatedAt         string                    `json:"created_at"`
	Lang              string                    `json:"lang"`
	PossiblySensitive bool                      `json:"possibly_sensitive"`
	User              syndicationUser           `json:"user"`
	MediaDetails      []syndicationMedia        `json:"mediaDetails"`
	Entities          syndicationEntities       `json:"entities"`
	FavoriteCount     int                       `json:"favorite_count"`
	ConversationCount int                       `json:"conversation_count"`
	RetweetCount      int                       `json:"retweet_count"`
	QuoteCount        int                       `json:"quote_count"`
	QuotedTweet       *syndicationTweetRelation `json:"quoted_tweet"`
	Parent            *syndicationTweetRelation `json:"parent"`
}

type syndicationUser struct {
	IDStr           string `json:"id_str"`
	Name            string `json:"name"`
	ScreenName      string `json:"screen_name"`
	ProfileImageURL string `json:"profile_image_url_https"`
	Verified        bool   `json:"verified"`
}

type syndicationMedia struct {
	Type          string               `json:"type"`
	MediaURLHTTPS string               `json:"media_url_https"`
	VideoInfo     syndicationVideoInfo `json:"video_info"`
}

type syndicationVideoInfo struct {
	Variants []syndicationVideoVariant `json:"variants"`
}

type syndicationVideoVariant struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Bitrate     int    `json:"bitrate"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type syndicationEntities struct {
	Hashtags []struct {
		Text string `json:"text"`
	} `json:"hashtags"`
}

type syndicationTweetRelation struct {
	IDStr string          `json:"id_str"`
	Text  string          `json:"text"`
	User  syndicationUser `json:"user"`
}

// MapSyndicationResponse maps only the public payload fields needed by the
// platform contract. Unmodelled embed fields never prevent a readable post
// from using the normal fallback chain.
func MapSyndicationResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed *syndicationTweetResponse) (*platform.Content, error) {
	if parsed == nil || strings.TrimSpace(parsed.IDStr) == "" {
		return nil, fmt.Errorf("syndication response missing post payload")
	}
	if norm != nil && strings.TrimSpace(norm.PlatformID) != "" && strings.TrimSpace(parsed.IDStr) != strings.TrimSpace(norm.PlatformID) {
		return nil, fmt.Errorf("syndication response post id does not match request")
	}
	media, variants, unavailable := syndicationMediaItems(parsed.MediaDetails)
	text := strings.TrimSpace(parsed.Text)
	// A public X post may consist entirely of an attached photo, video, or GIF.
	// Keep those posts on the public syndication path even when there is no text;
	// an entirely empty payload still follows the established fallback chain.
	if text == "" && len(media) == 0 && unavailable == 0 {
		return nil, fmt.Errorf("syndication response has neither post text nor media")
	}
	author := parsed.User
	meta := map[string]any{
		"extraction_source": "syndication", "platform_id": parsed.IDStr,
		"lang": strings.TrimSpace(parsed.Lang), "possibly_sensitive": parsed.PossiblySensitive,
	}
	if norm != nil && len(norm.RiskFlags) > 0 {
		meta["risk_flags"] = norm.RiskFlags
	}
	if author.ScreenName != "" {
		meta["author_username"] = strings.TrimSpace(author.ScreenName)
	}
	if author.Verified {
		meta["author_verified"] = true
	}
	if relation := relationSummary(parsed.QuotedTweet); relation != nil {
		meta["quoted_tweet"] = relation
	}
	if relation := relationSummary(parsed.Parent); relation != nil {
		meta["parent"] = relation
	}
	if len(variants) > 0 {
		meta["x_media_variants"] = variants
	}
	if unavailable > 0 {
		meta["video_variant_unavailable"] = unavailable
	}
	c := &platform.Content{
		Platform: PlatformName, ContentType: ContentType, SourceURL: sourceURL(in, norm),
		CanonicalURL: canonicalSyndicationPostURL(norm, parsed.IDStr, author.ScreenName),
		Title:        titleFromTweetText(text), Text: text, Markdown: text, PublishedAt: strings.TrimSpace(parsed.CreatedAt),
		CapturedAt: platform.NowRFC3339(), Engagement: syndicationEngagement(parsed), Tags: syndicationTags(parsed.Entities), PlatformMetadata: meta,
	}
	if author.IDStr != "" || author.Name != "" || author.ScreenName != "" {
		c.Author = &platform.Author{ID: strings.TrimSpace(author.IDStr), Name: firstNonEmpty(author.Name, author.ScreenName), ProfileURL: profileURL(author.ScreenName), AvatarURL: strings.TrimSpace(author.ProfileImageURL)}
	}
	if in.IncludeMedia {
		c.Media = media
	}
	return c, nil
}

func canonicalSyndicationPostURL(norm *platform.NormalizedURL, id, username string) string {
	if username = strings.TrimSpace(username); username != "" {
		return fmt.Sprintf("https://x.com/%s/status/%s", username, strings.TrimSpace(id))
	}
	if norm != nil && strings.TrimSpace(norm.CanonicalURL) != "" {
		return norm.CanonicalURL
	}
	return fmt.Sprintf("https://x.com/i/status/%s", strings.TrimSpace(id))
}

func syndicationMediaItems(items []syndicationMedia) ([]platform.MediaItem, []map[string]any, int) {
	var videos, images []platform.MediaItem
	var variants []map[string]any
	unavailable := 0
	for _, item := range items {
		kind := strings.ToLower(strings.TrimSpace(item.Type))
		if kind == "photo" {
			if u := strings.TrimSpace(item.MediaURLHTTPS); u != "" {
				images = append(images, platform.MediaItem{Type: "image", URL: u})
			}
			continue
		}
		if kind != "video" && kind != "animated_gif" {
			continue
		}
		best, all := bestSyndicationMP4(item.VideoInfo.Variants)
		if len(all) > 0 {
			variants = append(variants, map[string]any{"type": kind, "variants": all})
		}
		if best.URL == "" {
			unavailable++
			continue
		}
		videos = append(videos, platform.MediaItem{Type: "video", URL: best.URL})
		if cover := strings.TrimSpace(item.MediaURLHTTPS); cover != "" {
			images = append(images, platform.MediaItem{Type: "image", URL: cover})
		}
	}
	return append(videos, images...), variants, unavailable
}

func bestSyndicationMP4(items []syndicationVideoVariant) (syndicationVideoVariant, []map[string]any) {
	valid := make([]syndicationVideoVariant, 0, len(items))
	all := make([]map[string]any, 0, len(items))
	for _, item := range items {
		url := strings.TrimSpace(item.URL)
		if url == "" {
			continue
		}
		all = append(all, map[string]any{"url": url, "content_type": strings.TrimSpace(item.ContentType), "bitrate": item.Bitrate, "width": item.Width, "height": item.Height})
		if strings.EqualFold(strings.TrimSpace(item.ContentType), "video/mp4") {
			valid = append(valid, item)
		}
	}
	if len(valid) == 0 {
		return syndicationVideoVariant{}, all
	}
	sort.SliceStable(valid, func(i, j int) bool {
		if valid[i].Bitrate != valid[j].Bitrate {
			return valid[i].Bitrate > valid[j].Bitrate
		}
		return valid[i].Width*valid[i].Height > valid[j].Width*valid[j].Height
	})
	return valid[0], all
}

func syndicationEngagement(in *syndicationTweetResponse) *platform.Engagement {
	e := &platform.Engagement{Likes: in.FavoriteCount, Comments: in.ConversationCount, Reposts: in.RetweetCount, Shares: in.QuoteCount}
	if e.Likes == 0 && e.Comments == 0 && e.Reposts == 0 && e.Shares == 0 {
		return nil
	}
	return e
}

func syndicationTags(in syndicationEntities) []string {
	seen := map[string]struct{}{}
	var tags []string
	for _, h := range in.Hashtags {
		if tag := strings.TrimSpace(h.Text); tag != "" {
			if _, ok := seen[tag]; !ok {
				seen[tag] = struct{}{}
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

func relationSummary(in *syndicationTweetRelation) map[string]any {
	if in == nil || strings.TrimSpace(in.IDStr) == "" {
		return nil
	}
	return map[string]any{"id": strings.TrimSpace(in.IDStr), "text": strings.TrimSpace(in.Text), "author_username": strings.TrimSpace(in.User.ScreenName)}
}
