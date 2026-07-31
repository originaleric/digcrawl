package source

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/feed"
	"github.com/originaleric/digcrawl/internal/platform"
	bilibiliplatform "github.com/originaleric/digcrawl/internal/platform/bilibili"
	douyinplatform "github.com/originaleric/digcrawl/internal/platform/douyin"
	instagramplatform "github.com/originaleric/digcrawl/internal/platform/instagram"
	kuaishouplatform "github.com/originaleric/digcrawl/internal/platform/kuaishou"
	"github.com/originaleric/digcrawl/internal/platform/reddit"
	threadsplatform "github.com/originaleric/digcrawl/internal/platform/threads"
	toutiaoplatform "github.com/originaleric/digcrawl/internal/platform/toutiao"
	weiboplatform "github.com/originaleric/digcrawl/internal/platform/weibo"
	xplatform "github.com/originaleric/digcrawl/internal/platform/x"
	xhsplatform "github.com/originaleric/digcrawl/internal/platform/xiaohongshu"
	zhihuplatform "github.com/originaleric/digcrawl/internal/platform/zhihu"
)

type Service struct {
	Client *http.Client
}

var listKuaishouUserVideosViaAPI = kuaishouplatform.ListUserVideosViaAPI

type Source struct {
	ID       string
	Platform string
	Type     string
	URL      string
	Name     string
	Options  map[string]any
}

type Item struct {
	ID           string
	ContentKey   string
	URL          string
	CanonicalURL string
	Title        string
	Author       string
	PublishedAt  time.Time
	ContentType  string
	Raw          map[string]any
}

func (r Service) ListLatest(ctx context.Context, item Source) ([]Item, error) {
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	switch {
	case platform == "rss" || typ == "rss" || typ == "feed":
		return r.readRSS(ctx, item)
	case typ == "sitemap" || platform == "sitemap":
		return r.readSitemap(ctx, item)
	case platform == "toutiao":
		return r.readToutiao(ctx, item)
	case platform == "wechat" || platform == "weixin":
		return r.readWechat(ctx, item)
	case platform == "xiaohongshu" || platform == "xhs":
		return r.readXiaohongshu(ctx, item)
	case platform == "douyin":
		return r.readDouyin(ctx, item)
	case platform == "bilibili" || platform == "bili":
		return r.readBilibili(ctx, item)
	case platform == "weibo":
		return r.readWeibo(ctx, item)
	case platform == "kuaishou" || platform == "ks":
		return r.readKuaishou(ctx, item)
	case platform == "zhihu":
		return r.readZhihu(ctx, item)
	case platform == "reddit":
		return r.readReddit(ctx, item)
	case platform == "x" || platform == "twitter":
		return r.readX(ctx, item)
	case platform == "threads":
		return r.readThreads(ctx, item)
	case platform == "instagram":
		return r.readInstagram(ctx, item)
	case platform == "web":
		if typ == "web_list" || typ == "list" || typ == "html_links" {
			return r.readHTMLLinks(ctx, item, "web")
		}
		if strings.Contains(strings.ToLower(item.URL), "sitemap") {
			return r.readSitemap(ctx, item)
		}
		return r.readRSS(ctx, item)
	case typ == "web_list" || typ == "list" || typ == "html_links":
		return r.readHTMLLinks(ctx, item, "web")
	default:
		return nil, fmt.Errorf("unsupported source platform %q", item.Platform)
	}
}

func (r Service) readToutiao(ctx context.Context, item Source) ([]Item, error) {
	switch inferToutiaoWatchlistType(ctx, item) {
	case "article", "group", "post", "toutiao_article":
		return feedItemFromToutiaoArticle(ctx, item)
	case "video", "toutiao_video":
		return feedItemFromToutiaoVideo(ctx, item)
	case "author", "user", "profile", "toutiao_author", "toutiao_user":
		return r.readHTMLLinks(ctx, item, "toutiao")
	default:
		if isToutiaoContentURL(ctx, item.URL, "article", "group") {
			return feedItemFromToutiaoArticle(ctx, item)
		}
		if isToutiaoContentURL(ctx, item.URL, "video") {
			return feedItemFromToutiaoVideo(ctx, item)
		}
		return r.readHTMLLinks(ctx, item, "toutiao")
	}
}

func inferToutiaoWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isToutiaoContentURL(ctx, item.URL, "article", "group") {
		return "article"
	}
	if isToutiaoContentURL(ctx, item.URL, "video") {
		return "video"
	}
	return "author"
}

func isToutiaoArticleURL(ctx context.Context, rawURL string) bool {
	return isToutiaoContentURL(ctx, rawURL, "article", "group")
}

func isToutiaoContentURL(ctx context.Context, rawURL string, kinds ...string) bool {
	norm, err := toutiaoplatform.NormalizeURL(ctx, rawURL)
	if err != nil || norm.PlatformID == "" {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(norm.ContentKind))
	for _, want := range kinds {
		if kind == strings.ToLower(strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func feedItemFromToutiaoArticle(ctx context.Context, item Source) ([]Item, error) {
	norm, err := toutiaoplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" {
		return nil, fmt.Errorf("toutiao article url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "toutiao:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "article",
		Raw: map[string]any{
			"article_id": norm.PlatformID,
			"source_url": item.URL,
		},
	}}, nil
}

func feedItemFromToutiaoVideo(ctx context.Context, item Source) ([]Item, error) {
	norm, err := toutiaoplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind != "video" {
		return nil, fmt.Errorf("toutiao video url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "toutiao:video:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "video",
		Raw: map[string]any{
			"video_id":   norm.PlatformID,
			"source_url": item.URL,
		},
	}}, nil
}

func (r Service) readWechat(ctx context.Context, item Source) ([]Item, error) {
	switch inferWechatWatchlistType(item) {
	case "wechat_article", "article", "post":
		return feedItemFromWechatArticle(item)
	case "wechat_list", "wechat_account", "account", "author", "profile", "user", "list":
		return r.readHTMLLinks(ctx, item, "wechat")
	default:
		if isWechatArticleURL(item.URL) {
			return feedItemFromWechatArticle(item)
		}
		return r.readHTMLLinks(ctx, item, "wechat")
	}
}

func inferWechatWatchlistType(item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isWechatArticleURL(item.URL) {
		return "wechat_article"
	}
	return "wechat_list"
}

func isWechatArticleURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	return (host == "mp.weixin.qq.com" || host == "weixin.qq.com") && (path == "/s" || strings.HasPrefix(path, "/s/"))
}

func feedItemFromWechatArticle(item Source) ([]Item, error) {
	link := canonicalWechatArticleURL(item.URL)
	if link == "" || !isWechatArticleURL(link) {
		return nil, fmt.Errorf("wechat article url is required")
	}
	key := "wechat:" + hashKey(link)
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "article",
		Raw: map[string]any{
			"source_url": item.URL,
		},
	}}, nil
}

func (r Service) readXiaohongshu(ctx context.Context, item Source) ([]Item, error) {
	switch inferXiaohongshuWatchlistType(ctx, item) {
	case "xiaohongshu_note", "xhs_note", "note", "post":
		return feedItemFromXiaohongshuNote(ctx, item)
	case "xiaohongshu_user", "xhs_user", "user", "profile", "author", "list":
		return r.readHTMLLinks(ctx, item, "xiaohongshu")
	default:
		if isXiaohongshuNoteURL(ctx, item.URL) {
			return feedItemFromXiaohongshuNote(ctx, item)
		}
		return r.readHTMLLinks(ctx, item, "xiaohongshu")
	}
}

func inferXiaohongshuWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isXiaohongshuNoteURL(ctx, item.URL) {
		return "xiaohongshu_note"
	}
	return "xiaohongshu_user"
}

func isXiaohongshuNoteURL(ctx context.Context, rawURL string) bool {
	norm, err := xhsplatform.NormalizeURL(ctx, rawURL)
	return err == nil && norm.PlatformID != ""
}

func feedItemFromXiaohongshuNote(ctx context.Context, item Source) ([]Item, error) {
	norm, err := xhsplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" {
		return nil, fmt.Errorf("xiaohongshu note url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "xiaohongshu:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "note",
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		},
	}}, nil
}

func (r Service) readDouyin(ctx context.Context, item Source) ([]Item, error) {
	switch inferDouyinWatchlistType(ctx, item) {
	case "douyin_video", "video", "douyin_note", "note", "post":
		return feedItemFromDouyinWork(ctx, item)
	case "douyin_user", "user", "profile", "author", "list":
		return r.readHTMLLinks(ctx, item, "douyin")
	default:
		if isDouyinWorkURL(ctx, item.URL) {
			return feedItemFromDouyinWork(ctx, item)
		}
		return r.readHTMLLinks(ctx, item, "douyin")
	}
}

func inferDouyinWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isDouyinWorkURL(ctx, item.URL) {
		return "douyin_video"
	}
	return "douyin_user"
}

func isDouyinWorkURL(ctx context.Context, rawURL string) bool {
	norm, err := douyinplatform.NormalizeURL(ctx, rawURL)
	return err == nil && norm.PlatformID != ""
}

func feedItemFromDouyinWork(ctx context.Context, item Source) ([]Item, error) {
	norm, err := douyinplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" {
		return nil, fmt.Errorf("douyin work url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	contentType := firstNonEmpty(norm.ContentKind, "video")
	key := "douyin:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  contentType,
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		},
	}}, nil
}

func (r Service) readX(ctx context.Context, item Source) ([]Item, error) {
	switch inferXWatchlistType(item) {
	case "x_post", "post", "status", "tweet":
		return feedItemFromXPost(item)
	case "x_user", "user", "profile", "account":
		return r.readHTMLLinks(ctx, item, "x")
	default:
		if isXStatusURL(item.URL) {
			return feedItemFromXPost(item)
		}
		return r.readHTMLLinks(ctx, item, "x")
	}
}

func (r Service) readThreads(ctx context.Context, item Source) ([]Item, error) {
	switch inferThreadsWatchlistType(item) {
	case "threads_post", "post", "thread":
		return feedItemFromThreadsPost(item)
	case "threads_user", "user", "profile", "account", "list":
		return r.readSocialHTMLLinks(ctx, item, "threads", `a[href*="/post/"]`, "body")
	default:
		if threadsplatform.IsPostURL(item.URL) {
			return feedItemFromThreadsPost(item)
		}
		return r.readSocialHTMLLinks(ctx, item, "threads", `a[href*="/post/"]`, "body")
	}
}

func inferThreadsWatchlistType(item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if threadsplatform.IsPostURL(item.URL) {
		return "threads_post"
	}
	return "threads_user"
}

func feedItemFromThreadsPost(item Source) ([]Item, error) {
	norm, err := threadsplatform.NormalizeURL(item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind != "post" {
		return nil, fmt.Errorf("threads post url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "threads:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		Author:       threadsUsernameFromURL(link),
		ContentType:  "thread",
		Raw: socialRaw(item, "threads", map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		}),
	}}, nil
}

func (r Service) readInstagram(ctx context.Context, item Source) ([]Item, error) {
	switch inferInstagramWatchlistType(item) {
	case "instagram_post", "post", "media", "instagram_reel", "reel":
		return feedItemFromInstagramMedia(item)
	case "instagram_user", "user", "profile", "account", "list":
		return r.readSocialHTMLLinks(ctx, item, "instagram", `a[href^="/p/"], a[href^="/reel/"], a[href*="/p/"], a[href*="/reel/"]`, "body")
	default:
		if instagramplatform.IsMediaURL(item.URL) {
			return feedItemFromInstagramMedia(item)
		}
		return r.readSocialHTMLLinks(ctx, item, "instagram", `a[href^="/p/"], a[href^="/reel/"], a[href*="/p/"], a[href*="/reel/"]`, "body")
	}
}

func inferInstagramWatchlistType(item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if instagramplatform.IsMediaURL(item.URL) {
		return "instagram_post"
	}
	return "instagram_user"
}

func feedItemFromInstagramMedia(item Source) ([]Item, error) {
	norm, err := instagramplatform.NormalizeURL(item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind == "user" {
		return nil, fmt.Errorf("instagram media url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "instagram:" + norm.PlatformID
	contentType := "instagram_post"
	if norm.ContentKind == "reel" {
		contentType = "video"
	}
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  contentType,
		Raw: socialRaw(item, "instagram", map[string]any{
			"platform_id":  norm.PlatformID,
			"source_url":   item.URL,
			"content_kind": norm.ContentKind,
		}),
	}}, nil
}

func (r Service) readBilibili(ctx context.Context, item Source) ([]Item, error) {
	switch inferBilibiliWatchlistType(ctx, item) {
	case "bilibili_video", "bili_video", "video", "post":
		return feedItemFromBilibiliVideo(ctx, item)
	case "bilibili_user", "bili_user", "user", "profile", "author", "list":
		return r.readBilibiliUserList(ctx, item)
	default:
		if isBilibiliVideoURL(ctx, item.URL) {
			return feedItemFromBilibiliVideo(ctx, item)
		}
		return r.readBilibiliUserList(ctx, item)
	}
}

func inferBilibiliWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isBilibiliVideoURL(ctx, item.URL) {
		return "bilibili_video"
	}
	return "bilibili_user"
}

func isBilibiliVideoURL(ctx context.Context, rawURL string) bool {
	norm, err := bilibiliplatform.NormalizeURL(ctx, rawURL)
	return err == nil && norm.PlatformID != "" && norm.ContentKind != "user"
}

func feedItemFromBilibiliVideo(ctx context.Context, item Source) ([]Item, error) {
	norm, err := bilibiliplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind == "user" {
		return nil, fmt.Errorf("bilibili video url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "bilibili:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "video",
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		},
	}}, nil
}

func (r Service) readBilibiliUserList(ctx context.Context, item Source) ([]Item, error) {
	apiItems, apiErr := r.fetchBilibiliUserVideos(ctx, item)
	if apiErr == nil && len(apiItems) > 0 {
		return apiItems, nil
	}
	item = withBrowserListDefaults(item, `a[href*="/video/"]`, "body")
	htmlItems, htmlErr := r.readHTMLLinks(ctx, item, "bilibili")
	if htmlErr == nil {
		return htmlItems, nil
	}
	if apiErr != nil {
		return nil, fmt.Errorf("%v; html fallback: %w", apiErr, htmlErr)
	}
	return nil, htmlErr
}

type bilibiliArcListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List struct {
			VList []bilibiliArcListItem `json:"vlist"`
		} `json:"list"`
	} `json:"data"`
}

type bilibiliArcListItem struct {
	BVID        string `json:"bvid"`
	AID         int64  `json:"aid"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	MID         int64  `json:"mid"`
	Created     int64  `json:"created"`
	Pic         string `json:"pic"`
	Description string `json:"description"`
	Play        int    `json:"play"`
	Comment     int    `json:"comment"`
	Favorites   int    `json:"favorites"`
}

func (r Service) fetchBilibiliUserVideos(ctx context.Context, item Source) ([]Item, error) {
	mid := firstNonEmpty(optionString(item.Options, "mid"), optionString(item.Options, "platform_id"), optionString(item.Options, "user_id"))
	if mid == "" {
		if norm, err := bilibiliplatform.NormalizeURL(ctx, item.URL); err == nil && norm.ContentKind == "user" {
			mid = norm.PlatformID
		}
	}
	if mid == "" {
		return nil, fmt.Errorf("missing bilibili user id")
	}
	limit := sourceLimit(item.Options, 20, 50)
	baseURL := firstNonEmpty(optionString(item.Options, "bilibili_api_base_url"), optionString(item.Options, "api_base_url"), "https://api.bilibili.com")
	endpointURL, err := url.Parse(strings.TrimRight(baseURL, "/") + "/x/space/wbi/arc/search")
	if err != nil {
		return nil, err
	}
	q := endpointURL.Query()
	q.Set("mid", mid)
	q.Set("pn", "1")
	q.Set("ps", strconv.Itoa(limit))
	q.Set("order", "pubdate")
	endpointURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", "https://space.bilibili.com/"+mid)
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl Watchlist")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed bilibiliArcListResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	attempt := platform.ClassifyAPIResponse("bilibili", resp.StatusCode, map[string]any{
		"code":       parsed.Code,
		"message":    parsed.Message,
		"data_count": len(parsed.Data.List.VList),
	}, func(map[string]any) bool {
		return len(parsed.Data.List.VList) == 0
	})
	attempt.Meta["api_endpoint"] = endpointURL.String()
	attempt.Meta["api_fetch_mode"] = "http"
	if !attempt.OK {
		return nil, attempt.Error()
	}
	out := make([]Item, 0, len(parsed.Data.List.VList))
	for _, v := range parsed.Data.List.VList {
		id := firstNonEmpty(v.BVID, strconv.FormatInt(v.AID, 10))
		if id == "" {
			continue
		}
		canonical := "https://www.bilibili.com/video/" + id
		if v.BVID == "" && v.AID > 0 {
			canonical = "https://www.bilibili.com/video/av" + strconv.FormatInt(v.AID, 10)
		}
		published := time.Time{}
		if v.Created > 0 {
			published = time.Unix(v.Created, 0)
		}
		out = append(out, Item{
			ID:           "bilibili:" + id,
			ContentKey:   "bilibili:" + id,
			URL:          canonical,
			CanonicalURL: canonical,
			Title:        strings.TrimSpace(v.Title),
			Author:       strings.TrimSpace(v.Author),
			PublishedAt:  published,
			ContentType:  "video",
			Raw: map[string]any{
				"platform_id":       id,
				"source_url":        item.URL,
				"author_mid":        v.MID,
				"cover":             v.Pic,
				"description":       v.Description,
				"extraction_source": "api",
			},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no bilibili videos found")
	}
	return out, nil
}

func (r Service) readWeibo(ctx context.Context, item Source) ([]Item, error) {
	switch inferWeiboWatchlistType(ctx, item) {
	case "weibo_post", "post", "status":
		return feedItemFromWeiboPost(ctx, item)
	case "weibo_user", "user", "profile", "author", "list":
		return r.readWeiboUserList(ctx, item)
	default:
		if isWeiboPostURL(ctx, item.URL) {
			return feedItemFromWeiboPost(ctx, item)
		}
		return r.readWeiboUserList(ctx, item)
	}
}

func inferWeiboWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isWeiboPostURL(ctx, item.URL) {
		return "weibo_post"
	}
	return "weibo_user"
}

func isWeiboPostURL(ctx context.Context, rawURL string) bool {
	norm, err := weiboplatform.NormalizeURL(ctx, rawURL)
	return err == nil && norm.PlatformID != "" && norm.ContentKind != "user"
}

func feedItemFromWeiboPost(ctx context.Context, item Source) ([]Item, error) {
	norm, err := weiboplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind == "user" {
		return nil, fmt.Errorf("weibo post url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "weibo:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "post",
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		},
	}}, nil
}

func (r Service) readWeiboUserList(ctx context.Context, item Source) ([]Item, error) {
	apiItems, apiErr := r.fetchWeiboUserPosts(ctx, item)
	if apiErr == nil && len(apiItems) > 0 {
		return apiItems, nil
	}
	item = withBrowserListDefaults(item, `a[href*="/detail/"], a[href*="/status/"]`, "body")
	htmlItems, htmlErr := r.readHTMLLinks(ctx, item, "weibo")
	if htmlErr == nil {
		return htmlItems, nil
	}
	if apiErr != nil {
		return nil, fmt.Errorf("%v; html fallback: %w", apiErr, htmlErr)
	}
	return nil, htmlErr
}

type weiboTimelineResponse struct {
	OK      int    `json:"ok"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
	Data    struct {
		Cards []weiboTimelineCard `json:"cards"`
	} `json:"data"`
}

type weiboTimelineCard struct {
	Mblog *weiboTimelineMblog `json:"mblog"`
}

type weiboTimelineMblog struct {
	ID            string            `json:"id"`
	MID           string            `json:"mid"`
	Text          string            `json:"text"`
	TextRaw       string            `json:"text_raw"`
	CreatedAt     string            `json:"created_at"`
	User          weiboTimelineUser `json:"user"`
	RepostsCount  int               `json:"reposts_count"`
	CommentsCount int               `json:"comments_count"`
	Attitudes     int               `json:"attitudes_count"`
}

type weiboTimelineUser struct {
	ID         int64  `json:"id"`
	ScreenName string `json:"screen_name"`
}

func (r Service) fetchWeiboUserPosts(ctx context.Context, item Source) ([]Item, error) {
	uid := firstNonEmpty(optionString(item.Options, "uid"), optionString(item.Options, "platform_id"), optionString(item.Options, "user_id"))
	if uid == "" {
		if norm, err := weiboplatform.NormalizeURL(ctx, item.URL); err == nil && norm.ContentKind == "user" {
			uid = norm.PlatformID
		}
	}
	if uid == "" {
		return nil, fmt.Errorf("missing weibo user id")
	}
	limit := sourceLimit(item.Options, 20, 50)
	baseURL := firstNonEmpty(optionString(item.Options, "weibo_api_base_url"), optionString(item.Options, "api_base_url"), "https://m.weibo.cn")
	endpointURL, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api/container/getIndex")
	if err != nil {
		return nil, err
	}
	q := endpointURL.Query()
	q.Set("type", "uid")
	q.Set("value", uid)
	q.Set("containerid", "107603"+uid)
	endpointURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", "https://m.weibo.cn/u/"+uid)
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl Watchlist")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed weiboTimelineResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	attempt := platform.ClassifyAPIResponse("weibo", resp.StatusCode, map[string]any{
		"success":    parsed.OK == 1,
		"ok":         parsed.OK,
		"message":    firstNonEmpty(parsed.Msg, parsed.Message),
		"data_count": len(parsed.Data.Cards),
	}, func(map[string]any) bool {
		return len(parsed.Data.Cards) == 0
	})
	attempt.Meta["api_endpoint"] = endpointURL.String()
	attempt.Meta["api_fetch_mode"] = "http"
	if !attempt.OK {
		return nil, attempt.Error()
	}
	out := make([]Item, 0, len(parsed.Data.Cards))
	for _, card := range parsed.Data.Cards {
		if card.Mblog == nil {
			continue
		}
		post := card.Mblog
		id := firstNonEmpty(post.ID, post.MID)
		if id == "" {
			continue
		}
		text := firstNonEmpty(post.TextRaw, feed.TextFromHTML(post.Text))
		canonical := "https://m.weibo.cn/detail/" + id
		out = append(out, Item{
			ID:           "weibo:" + id,
			ContentKey:   "weibo:" + id,
			URL:          canonical,
			CanonicalURL: canonical,
			Title:        firstNonEmpty(shortText(text, 80), item.Name),
			Author:       strings.TrimSpace(post.User.ScreenName),
			PublishedAt:  parseTime(post.CreatedAt),
			ContentType:  "post",
			Raw: map[string]any{
				"platform_id":       id,
				"mid":               post.MID,
				"source_url":        item.URL,
				"author_id":         post.User.ID,
				"text":              text,
				"extraction_source": "api",
			},
		})
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no weibo posts found")
	}
	return out, nil
}

func (r Service) readKuaishou(ctx context.Context, item Source) ([]Item, error) {
	switch inferKuaishouWatchlistType(ctx, item) {
	case "kuaishou_video", "ks_video", "video", "post":
		return feedItemFromKuaishouVideo(ctx, item)
	case "kuaishou_user", "ks_user", "user", "profile", "author", "list":
		return r.readKuaishouUserList(ctx, item)
	default:
		if isKuaishouVideoURL(ctx, item.URL) {
			return feedItemFromKuaishouVideo(ctx, item)
		}
		return r.readKuaishouUserList(ctx, item)
	}
}

func inferKuaishouWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isKuaishouVideoURL(ctx, item.URL) {
		return "kuaishou_video"
	}
	return "kuaishou_user"
}

func isKuaishouVideoURL(ctx context.Context, rawURL string) bool {
	norm, err := kuaishouplatform.NormalizeURL(ctx, rawURL)
	return err == nil && norm.PlatformID != "" && norm.ContentKind != "user"
}

func feedItemFromKuaishouVideo(ctx context.Context, item Source) ([]Item, error) {
	norm, err := kuaishouplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind == "user" {
		return nil, fmt.Errorf("kuaishou video url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "kuaishou:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  "video",
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		},
	}}, nil
}

func (r Service) readKuaishouUserList(ctx context.Context, item Source) ([]Item, error) {
	items, err := r.readKuaishouUserListViaAPI(ctx, item)
	if err == nil && len(items) > 0 {
		return items, nil
	}
	if _, stop := platform.ShouldStopStrategy(err); stop {
		return nil, err
	}
	item = withBrowserListDefaults(item, `a[href*="/short-video/"], a[href*="/photo/"]`, "body")
	return r.readHTMLLinks(ctx, item, "kuaishou")
}

func (r Service) readKuaishouUserListViaAPI(ctx context.Context, item Source) ([]Item, error) {
	norm, err := kuaishouplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind != "user" {
		return nil, fmt.Errorf("kuaishou user url is required")
	}
	limit := optionInt(item.Options, "limit")
	if limit <= 0 {
		limit = 20
	}
	apiItems, _, _, err := listKuaishouUserVideosViaAPI(ctx, norm, platform.ReadInput{
		URL:             item.URL,
		UseCookieDomain: optionString(item.Options, "use_cookie_domain"),
	}, limit)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(apiItems))
	for _, apiItem := range apiItems {
		if strings.TrimSpace(apiItem.ID) == "" || strings.TrimSpace(apiItem.CanonicalURL) == "" {
			continue
		}
		raw := socialRaw(item, "kuaishou", apiItem.Raw)
		if !apiItem.PublishedAt.IsZero() {
			raw["published_at"] = apiItem.PublishedAt.Format(time.RFC3339)
		}
		items = append(items, Item{
			ID:           apiItem.ID,
			ContentKey:   apiItem.ID,
			URL:          apiItem.CanonicalURL,
			CanonicalURL: apiItem.CanonicalURL,
			Title:        apiItem.Title,
			ContentType:  apiItem.ContentType,
			PublishedAt:  apiItem.PublishedAt,
			Raw:          raw,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no kuaishou api videos found")
	}
	return items, nil
}

func (r Service) readZhihu(ctx context.Context, item Source) ([]Item, error) {
	switch inferZhihuWatchlistType(ctx, item) {
	case "zhihu_answer", "answer", "zhihu_article", "article", "zhihu_zvideo", "zvideo", "video", "question":
		return feedItemFromZhihuContent(ctx, item)
	case "zhihu_user", "user", "profile", "author", "list":
		return r.readZhihuUserList(ctx, item)
	default:
		if isZhihuContentURL(ctx, item.URL) {
			return feedItemFromZhihuContent(ctx, item)
		}
		return r.readZhihuUserList(ctx, item)
	}
}

func inferZhihuWatchlistType(ctx context.Context, item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isZhihuContentURL(ctx, item.URL) {
		return "zhihu_content"
	}
	return "zhihu_user"
}

func isZhihuContentURL(ctx context.Context, rawURL string) bool {
	norm, err := zhihuplatform.NormalizeURL(ctx, rawURL)
	return err == nil && norm.PlatformID != "" && norm.ContentKind != "user"
}

func feedItemFromZhihuContent(ctx context.Context, item Source) ([]Item, error) {
	norm, err := zhihuplatform.NormalizeURL(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" || norm.ContentKind == "user" {
		return nil, fmt.Errorf("zhihu content url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "zhihu:" + norm.ContentKind + ":" + norm.PlatformID
	contentType := norm.ContentKind
	if contentType == "question" {
		contentType = "article"
	}
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		ContentType:  contentType,
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
			"source_url":  item.URL,
		},
	}}, nil
}

func (r Service) readZhihuUserList(ctx context.Context, item Source) ([]Item, error) {
	item = withBrowserListDefaults(item, `a[href*="/question/"], a[href*="/p/"], a[href*="/zvideo/"]`, "body")
	return r.readHTMLLinks(ctx, item, "zhihu")
}

func inferXWatchlistType(item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	if isXStatusURL(item.URL) {
		return "x_post"
	}
	return "x_user"
}

func isXStatusURL(rawURL string) bool {
	norm, err := xplatform.NormalizeURL(rawURL)
	return err == nil && norm.PlatformID != ""
}

func feedItemFromXPost(item Source) ([]Item, error) {
	norm, err := xplatform.NormalizeURL(item.URL)
	if err != nil {
		return nil, err
	}
	if norm.PlatformID == "" {
		return nil, fmt.Errorf("x post url is required")
	}
	link := firstNonEmpty(norm.CanonicalURL, item.URL)
	key := "x:" + norm.PlatformID
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          link,
		CanonicalURL: link,
		Title:        item.Name,
		Author:       xUsernameFromURL(link),
		ContentType:  "x_post",
		Raw: map[string]any{
			"platform_id": norm.PlatformID,
		},
	}}, nil
}

func (r Service) readReddit(ctx context.Context, item Source) ([]Item, error) {
	switch inferRedditWatchlistType(item) {
	case "reddit_subreddit", "subreddit", "community":
		return r.readRedditSubreddit(ctx, item)
	case "reddit_user", "user":
		return r.readRedditUser(ctx, item)
	case "reddit_post_comments", "post_comments", "comments":
		return r.readRedditPostComments(ctx, item)
	default:
		return nil, fmt.Errorf("unsupported reddit source type %q", item.Type)
	}
}

func inferRedditWatchlistType(item Source) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	urlLower := strings.ToLower(item.URL)
	switch {
	case reddit.IsPostURL(item.URL):
		return "reddit_post_comments"
	case optionString(item.Options, "subreddit") != "" || strings.Contains(urlLower, "/r/"):
		return "reddit_subreddit"
	case optionString(item.Options, "username") != "" || strings.Contains(urlLower, "/user/") || strings.Contains(urlLower, "/u/"):
		return "reddit_user"
	default:
		return ""
	}
}

func (r Service) readRedditSubreddit(ctx context.Context, item Source) ([]Item, error) {
	sub := firstNonEmpty(optionString(item.Options, "subreddit"), redditSubredditFromURL(item.URL))
	if sub == "" {
		return nil, fmt.Errorf("subreddit or subreddit url is required")
	}
	res, err := reddit.ReadSubreddit(ctx, reddit.ListingOptions{
		Subreddit: sub,
		Sort:      optionString(item.Options, "sort"),
		TimeRange: optionString(item.Options, "time_range"),
		Limit:     optionInt(item.Options, "limit"),
		After:     optionString(item.Options, "after"),
	})
	if err != nil {
		return nil, err
	}
	return feedItemsFromRedditListing(res), nil
}

func (r Service) readRedditUser(ctx context.Context, item Source) ([]Item, error) {
	user := firstNonEmpty(optionString(item.Options, "username"), redditUsernameFromURL(item.URL))
	if user == "" {
		return nil, fmt.Errorf("username or reddit user url is required")
	}
	res, err := reddit.ReadUser(ctx, reddit.ListingOptions{
		Username: user,
		Where:    firstNonEmpty(optionString(item.Options, "where"), optionString(item.Options, "sort")),
		Limit:    optionInt(item.Options, "limit"),
		After:    optionString(item.Options, "after"),
	})
	if err != nil {
		return nil, err
	}
	return feedItemsFromRedditListing(res), nil
}

func (r Service) readRedditPostComments(ctx context.Context, item Source) ([]Item, error) {
	if !reddit.IsPostURL(item.URL) {
		return nil, fmt.Errorf("reddit post url is required")
	}
	limit := optionInt(item.Options, "comment_limit")
	if limit <= 0 {
		limit = optionInt(item.Options, "limit")
	}
	if limit <= 0 {
		limit = 1
	}
	depth := optionInt(item.Options, "comment_depth")
	content, err := reddit.ReadPost(ctx, reddit.ReadInput{
		ReadInput:       platform.ReadInput{URL: item.URL},
		IncludeComments: true,
		CommentLimit:    limit,
		CommentDepth:    depth,
		CommentSort:     firstNonEmpty(optionString(item.Options, "comment_sort"), optionString(item.Options, "sort"), "new"),
		ExpandMore:      optionBool(item.Options, "expand_more"),
		ExpandMoreLimit: optionInt(item.Options, "expand_more_limit"),
	})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: reddit post response is empty")
	}
	key := "reddit:comments:" + firstNonEmpty(platform.MetaString(content.PlatformMetadata, "fullname"), content.CanonicalURL, item.URL)
	latest := latestRedditComment(content.PlatformMetadata)
	if latest.ID != "" {
		key += ":t1_" + latest.ID
	}
	return []Item{{
		ID:           key,
		ContentKey:   key,
		URL:          item.URL,
		CanonicalURL: firstNonEmpty(content.CanonicalURL, item.URL),
		Title:        content.Title,
		Author:       commentAuthorOrPostAuthor(latest, content),
		PublishedAt:  timeFromUnix(latest.CreatedUTC),
		ContentType:  "reddit_post_comments",
		Raw: map[string]any{
			"latest_comment_id": latest.ID,
			"latest_comment":    latest.Body,
			"post_title":        content.Title,
		},
	}}, nil
}

func feedItemsFromRedditListing(res *reddit.ListingResult) []Item {
	if res == nil {
		return nil
	}
	out := make([]Item, 0, len(res.Items))
	for _, it := range res.Items {
		link := firstNonEmpty(it.CanonicalURL, it.URL)
		if link == "" {
			continue
		}
		contentType := "reddit_post"
		if it.Kind == "t1" {
			contentType = "reddit_comment"
		}
		out = append(out, Item{
			ID:           firstNonEmpty(it.Fullname, it.ID, hashKey(link)),
			ContentKey:   redditListingContentKey(it, link),
			URL:          link,
			CanonicalURL: link,
			Title:        it.Title,
			Author:       it.Author,
			PublishedAt:  timeFromUnix(it.CreatedUTC),
			ContentType:  contentType,
			Raw: map[string]any{
				"subreddit":    it.Subreddit,
				"score":        it.Score,
				"num_comments": it.NumComments,
				"kind":         it.Kind,
				"body":         it.Body,
			},
		})
	}
	return out
}

func redditListingContentKey(it reddit.ListingItem, link string) string {
	if it.ContentKey != "" {
		return it.ContentKey
	}
	if it.Fullname != "" {
		return "reddit:" + it.Fullname
	}
	return link
}

func (r Service) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (r Service) feedService() feed.Service {
	return feed.Service{Client: r.httpClient(), UserAgent: "Mozilla/5.0 DigCrawl Watchlist"}
}

func (r Service) readRSS(ctx context.Context, item Source) ([]Item, error) {
	doc, err := r.feedService().Read(ctx, item.URL)
	return watchItemsFromDocument(doc, "article"), err
}

func (r Service) readSitemap(ctx context.Context, item Source) ([]Item, error) {
	doc, err := r.feedService().ReadSitemap(ctx, item.URL)
	return watchItemsFromDocument(doc, "web"), err
}

func watchItemsFromDocument(doc *feed.Document, contentType string) []Item {
	if doc == nil {
		return nil
	}
	out := make([]Item, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		link := normalizeLink(nil, entry.URL)
		if link == "" {
			continue
		}
		published := entry.PublishedAt
		if published.IsZero() {
			published = entry.UpdatedAt
		}
		out = append(out, Item{
			ID:           firstNonEmpty(entry.ID, link),
			ContentKey:   firstNonEmpty(entry.ID, link),
			URL:          link,
			CanonicalURL: link,
			Title:        entry.Title,
			Author:       entry.Author,
			PublishedAt:  published,
			ContentType:  contentType,
			Raw: map[string]any{
				"summary": entry.SummaryHTML,
			},
		})
	}
	return out
}

var hrefRe = regexp.MustCompile(`(?i)href=["']([^"']+)["']`)
var absoluteURLRe = regexp.MustCompile(`(?i)https?://[^"'<>\\\s]+`)
var relativeContentPathRe = regexp.MustCompile(`(?i)/(?:article|group|video|w)/[0-9]+/?|/[A-Za-z0-9_]{1,32}/status/[0-9]+/?|/video/(?:BV[0-9A-Za-z]+|av[0-9]+)/?|/(?:detail|status)/[0-9A-Za-z]+/?|/(?:short-video|photo)/[0-9A-Za-z_-]+/?|/question/[0-9]+/answer/[0-9]+/?|/p/[0-9A-Za-z_-]+/?|/reel/[0-9A-Za-z_-]+/?|/@[A-Za-z0-9._]+/post/[0-9A-Za-z_-]+/?|/zvideo/[0-9]+/?`)

func (r Service) readHTMLLinks(ctx context.Context, item Source, platform string) ([]Item, error) {
	data, _, err := r.feedService().Fetch(ctx, item.URL)
	base, _ := url.Parse(item.URL)
	var staticErr error
	if err != nil {
		staticErr = err
	} else {
		html := string(data)
		lower := strings.ToLower(html)
		if strings.Contains(lower, "captcha") || strings.Contains(lower, "验证") {
			staticErr = fmt.Errorf("CAPTCHA_REQUIRED: platform page requires verification")
		} else if strings.Contains(lower, "login") && strings.Contains(lower, "password") {
			staticErr = fmt.Errorf("LOGIN_REQUIRED: platform page requires login")
		} else if out, err := r.itemsFromLinkCandidates(ctx, item, platform, base, linkCandidates(html)); err == nil {
			return out, nil
		} else {
			staticErr = err
		}
	}
	if browserFallbackEnabled(item.Options) {
		if out, err := r.readHTMLLinksBrowser(ctx, item, platform, base); err == nil {
			return out, nil
		} else if staticErr != nil {
			if platform == "toutiao" {
				if accessErr := r.toutiaoAuthorAccessError(ctx, item.URL); accessErr != nil {
					return nil, accessErr
				}
			}
			return nil, fmt.Errorf("%v; browser fallback: %w", staticErr, err)
		} else {
			if platform == "toutiao" {
				if accessErr := r.toutiaoAuthorAccessError(ctx, item.URL); accessErr != nil {
					return nil, accessErr
				}
			}
			return nil, err
		}
	}
	if platform == "toutiao" {
		if accessErr := r.toutiaoAuthorAccessError(ctx, item.URL); accessErr != nil {
			return nil, accessErr
		}
	}
	if staticErr != nil {
		return nil, staticErr
	}
	return nil, fmt.Errorf("CONTENT_NOT_FOUND: no %s content links found", platform)
}

func (r Service) readSocialHTMLLinks(ctx context.Context, item Source, platform, waitSelector, contentSelector string) ([]Item, error) {
	item = withBrowserListDefaults(item, waitSelector, contentSelector)
	socialCfg := socialPlatformConfig(platform)
	if socialCfg.RequestTimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(socialCfg.RequestTimeoutSec)*time.Second)
		defer cancel()
	}
	opts := map[string]any{}
	for k, v := range item.Options {
		opts[k] = v
	}
	if _, ok := optionBoolValue(opts, "browser_fallback"); !ok {
		opts["browser_fallback"] = socialCfg.BrowserFallbackOrDefault()
	}
	item.Options = opts
	return r.readHTMLLinks(ctx, item, platform)
}

func (r Service) itemsFromLinkCandidates(ctx context.Context, item Source, platform string, base *url.URL, candidates []string) ([]Item, error) {
	includeRe, err := compileOptionalPattern(optionString(item.Options, "include_pattern"))
	if err != nil {
		return nil, fmt.Errorf("invalid include_pattern: %w", err)
	}
	excludeRe, err := compileOptionalPattern(optionString(item.Options, "exclude_pattern"))
	if err != nil {
		return nil, fmt.Errorf("invalid exclude_pattern: %w", err)
	}
	limit := optionInt(item.Options, "limit")
	seen := map[string]bool{}
	var out []Item
	for _, candidate := range candidates {
		link := normalizeLink(base, candidate)
		if link == "" {
			continue
		}
		if !platformLink(platform, base, link) {
			continue
		}
		if includeRe != nil && !includeRe.MatchString(link) {
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(link) {
			continue
		}
		discovered := discoveredLinkItem(ctx, platform, link, item.Name)
		discovered.Raw = socialRaw(item, platform, discovered.Raw)
		seenKey := firstNonEmpty(discovered.ContentKey, discovered.CanonicalURL, discovered.URL)
		if seenKey == "" || seen[seenKey] {
			continue
		}
		seen[seenKey] = true
		out = append(out, discovered)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no %s content links found", platform)
	}
	rankDiscoveredItems(platform, out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func rankDiscoveredItems(platform string, items []Item) {
	if platform != "toutiao" || len(items) < 2 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, lok := toutiaoNumericContentID(items[i])
		right, rok := toutiaoNumericContentID(items[j])
		if !lok || !rok {
			return lok && !rok
		}
		return left > right
	})
}

func toutiaoNumericContentID(item Item) (uint64, bool) {
	for _, value := range []string{item.ContentKey, item.ID, item.CanonicalURL, item.URL} {
		if id, ok := trailingNumericToken(value); ok {
			return id, true
		}
	}
	return 0, false
}

func trailingNumericToken(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	end := len(value)
	for end > 0 {
		ch := value[end-1]
		if ch >= '0' && ch <= '9' {
			break
		}
		end--
	}
	start := end
	for start > 0 {
		ch := value[start-1]
		if ch < '0' || ch > '9' {
			break
		}
		start--
	}
	if start == end {
		return 0, false
	}
	id, err := strconv.ParseUint(value[start:end], 10, 64)
	return id, err == nil
}

func (r Service) readHTMLLinksBrowser(ctx context.Context, item Source, platform string, base *url.URL) ([]Item, error) {
	waitSelector := firstNonEmpty(optionString(item.Options, "wait_selector"), optionString(item.Options, "browser_wait_selector"), "body")
	contentSelector := firstNonEmpty(optionString(item.Options, "content_selector"), optionString(item.Options, "browser_content_selector"), "body")
	resp, err := browser.BrowserBrowse(ctx, &browser.BrowserBrowseRequest{
		URL:             item.URL,
		Action:          "read",
		Mode:            "full",
		WaitSelector:    waitSelector,
		ContentSelector: contentSelector,
		UseCookieDomain: browserCookieDomain(platform, item.URL, item.Options),
		MetadataScript:  discoveryMetadataScript(),
	})
	if err != nil {
		return nil, err
	}
	candidates := discoveryCandidatesFromBrowser(resp)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: browser found no %s content candidates", platform)
	}
	if resp != nil && strings.TrimSpace(resp.URL) != "" {
		if renderedBase, err := url.Parse(resp.URL); err == nil {
			base = renderedBase
		}
	}
	return r.itemsFromLinkCandidates(ctx, item, platform, base, candidates)
}

func discoveryCandidatesFromBrowser(resp *browser.BrowserBrowseResponse) []string {
	if resp == nil {
		return nil
	}
	var out []string
	if meta := resp.Metadata; meta != nil {
		out = append(out, stringSliceFromAny(meta["hrefs"])...)
		out = append(out, stringFromAny(meta["url"]))
		out = append(out, stringFromAny(meta["text"]))
		out = append(out, stringFromAny(meta["scripts"]))
	}
	out = append(out, resp.URL, resp.Text, resp.Markdown)
	return out
}

func discoveryMetadataScript() string {
	return `() => {
  const hrefs = Array.from(document.querySelectorAll('a[href]')).map(a => a.href).filter(Boolean);
  const scripts = Array.from(document.scripts).map(s => s.textContent || s.src || '').join('\n').slice(0, 200000);
  const text = (document.body && document.body.innerText || '').slice(0, 100000);
  return { hrefs, scripts, text, url: String(location.href || '') };
}`
}

func browserFallbackEnabled(opts map[string]any) bool {
	if value, ok := optionBoolValue(opts, "browser_fallback"); ok {
		return value
	}
	if value, ok := optionBoolValue(opts, "browser_fallback_enabled"); ok {
		return value
	}
	return true
}

func withBrowserListDefaults(item Source, waitSelector, contentSelector string) Source {
	if strings.TrimSpace(waitSelector) == "" && strings.TrimSpace(contentSelector) == "" {
		return item
	}
	opts := map[string]any{}
	for k, v := range item.Options {
		opts[k] = v
	}
	if _, ok := opts["wait_selector"]; !ok && optionString(opts, "browser_wait_selector") == "" && strings.TrimSpace(waitSelector) != "" {
		opts["wait_selector"] = waitSelector
	}
	if _, ok := opts["content_selector"]; !ok && optionString(opts, "browser_content_selector") == "" && strings.TrimSpace(contentSelector) != "" {
		opts["content_selector"] = contentSelector
	}
	item.Options = opts
	return item
}

func browserCookieDomain(platform, rawURL string, opts map[string]any) string {
	switch platform {
	case "toutiao":
		return "www.toutiao.com"
	case "x", "twitter":
		return "x.com"
	case "douyin":
		return "www.douyin.com"
	case "xiaohongshu", "xhs":
		return "www.xiaohongshu.com"
	case "wechat", "weixin":
		return "mp.weixin.qq.com"
	case "bilibili", "bili":
		return "www.bilibili.com"
	case "weibo":
		return "m.weibo.cn"
	case "kuaishou", "ks":
		return "www.kuaishou.com"
	case "zhihu":
		return "www.zhihu.com"
	case "threads":
		if socialLocalSessionAllowed(platform, opts) {
			return threadsplatform.CookieDomain
		}
		return ""
	case "instagram":
		if socialLocalSessionAllowed(platform, opts) {
			return instagramplatform.CookieDomain
		}
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Hostname())
}

func socialPlatformConfig(platform string) config.SocialPlatformConfig {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "threads":
		return config.Get().Social.Threads
	case "instagram":
		return config.Get().Social.Instagram
	default:
		return config.SocialPlatformConfig{}
	}
}

func socialSourceAuthMode(opts map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(optionString(opts, "auth_mode")))
	if mode == "local_session" {
		return "local_session"
	}
	return "public"
}

func socialLocalSessionAllowed(platform string, opts map[string]any) bool {
	return socialSourceAuthMode(opts) == "local_session" && socialPlatformConfig(platform).AllowsLocalSession()
}

func socialRaw(item Source, platform string, base map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "threads", "instagram":
	default:
		return base
	}
	if socialLocalSessionAllowed(platform, item.Options) {
		base["use_cookie_domain"] = browserCookieDomain(platform, item.URL, item.Options)
		base["auth_mode"] = "local_session"
	} else {
		base["auth_mode"] = "public"
	}
	return base
}

func (r Service) toutiaoAuthorAccessError(ctx context.Context, rawURL string) error {
	token := toutiaoUserTokenFromURL(rawURL)
	if token == "" {
		return nil
	}
	apiURL := "https://www.toutiao.com/api/pc/user/info?token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", "https://www.toutiao.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl Watchlist")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var payload struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
		UserID  int64  `json:"user_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}
	reason := strings.ToLower(strings.TrimSpace(payload.Reason))
	message := strings.ToLower(strings.TrimSpace(payload.Message))
	if reason == "login_required" || (payload.UserID == 0 && message == "error") {
		return fmt.Errorf("LOGIN_REQUIRED: toutiao author page requires login")
	}
	return nil
}

func toutiaoUserTokenFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "token") {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

func linkCandidates(html string) []string {
	expanded := strings.NewReplacer(
		`\\u002F`, "/",
		`\u002F`, "/",
		`\/`, "/",
		`\\u0026`, "&",
		`\u0026`, "&",
		"&amp;", "&",
	).Replace(html)
	var out []string
	for _, m := range hrefRe.FindAllStringSubmatch(expanded, -1) {
		out = append(out, m[1])
	}
	out = append(out, absoluteURLRe.FindAllString(expanded, -1)...)
	out = append(out, relativeContentPathRe.FindAllString(expanded, -1)...)
	return out
}

func normalizeLink(base *url.URL, raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\u0026", "&"))
	if raw == "" || strings.HasPrefix(raw, "javascript:") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	u.Fragment = ""
	stripTrackingQuery(u)
	return u.String()
}

func stripTrackingQuery(u *url.URL) {
	if u == nil || u.RawQuery == "" {
		return
	}
	q := u.Query()
	for key := range q {
		lower := strings.ToLower(strings.TrimSpace(key))
		if strings.HasPrefix(lower, "utm_") || lower == "spm" || lower == "spm_id_from" || lower == "fbclid" || lower == "gclid" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
}

func canonicalWechatArticleURL(raw string) string {
	link := normalizeLink(nil, raw)
	if link == "" {
		return ""
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if !isWechatArticleURL(u.String()) {
		return link
	}
	path := strings.ToLower(u.Path)
	if strings.HasPrefix(path, "/s/") {
		u.RawQuery = ""
		return u.String()
	}
	q := u.Query()
	stable := url.Values{}
	for _, key := range []string{"__biz", "mid", "idx", "sn", "chksm"} {
		if value := strings.TrimSpace(q.Get(key)); value != "" {
			stable.Set(key, value)
		}
	}
	if len(stable) > 0 {
		u.RawQuery = stable.Encode()
	}
	return u.String()
}

func discoveredLinkItem(ctx context.Context, platform, link, title string) Item {
	id := hashKey(link)
	key := link
	canonical := link
	switch platform {
	case "toutiao":
		if norm, err := toutiaoplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" {
			if norm.ContentKind == "video" {
				id = "toutiao:video:" + norm.PlatformID
			} else {
				id = "toutiao:" + norm.PlatformID
			}
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "douyin":
		if norm, err := douyinplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" {
			id = "douyin:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "bilibili":
		if norm, err := bilibiliplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" && norm.ContentKind != "user" {
			id = "bilibili:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "weibo":
		if norm, err := weiboplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" && norm.ContentKind != "user" {
			id = "weibo:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "kuaishou":
		if norm, err := kuaishouplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" && norm.ContentKind != "user" {
			id = "kuaishou:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "zhihu":
		if norm, err := zhihuplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" && norm.ContentKind != "user" {
			id = "zhihu:" + norm.ContentKind + ":" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "xiaohongshu", "xhs":
		if norm, err := xhsplatform.NormalizeURL(ctx, link); err == nil && norm.PlatformID != "" {
			id = "xiaohongshu:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "x", "twitter":
		if norm, err := xplatform.NormalizeURL(link); err == nil && norm.PlatformID != "" {
			id = "x:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "threads":
		if norm, err := threadsplatform.NormalizeURL(link); err == nil && norm.PlatformID != "" && norm.ContentKind == "post" {
			id = "threads:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "instagram":
		if norm, err := instagramplatform.NormalizeURL(link); err == nil && norm.PlatformID != "" && norm.ContentKind != "user" {
			id = "instagram:" + norm.PlatformID
			key = id
			canonical = firstNonEmpty(norm.CanonicalURL, link)
		}
	case "wechat", "weixin":
		if isWechatArticleURL(link) {
			canonical = canonicalWechatArticleURL(link)
			id = "wechat:" + hashKey(canonical)
			key = id
		}
	}
	return Item{
		ID:           id,
		ContentKey:   key,
		URL:          canonical,
		CanonicalURL: canonical,
		Title:        title,
		ContentType:  contentTypeFor(platform, canonical),
		Raw:          socialRaw(Source{URL: canonical}, platform, nil),
	}
}

func compileOptionalPattern(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

func platformLink(platform string, base *url.URL, link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	switch platform {
	case "toutiao":
		return strings.Contains(host, "toutiao.com") && (strings.Contains(path, "/article/") || strings.Contains(path, "/group/") || strings.Contains(path, "/video/") || strings.Contains(path, "/w/"))
	case "douyin":
		return (strings.Contains(host, "douyin.com") || strings.Contains(host, "iesdouyin.com")) && (strings.Contains(path, "/video/") || strings.Contains(path, "/note/") || strings.Contains(path, "/share/video/"))
	case "bilibili":
		return (strings.Contains(host, "bilibili.com") || host == "b23.tv" || strings.HasSuffix(host, ".b23.tv")) && strings.Contains(path, "/video/")
	case "weibo":
		return (host == "m.weibo.cn" || strings.HasSuffix(host, ".weibo.com") || host == "weibo.com") && (strings.Contains(path, "/detail/") || strings.Contains(path, "/status/") || webWeiboPostPath(path))
	case "kuaishou":
		return (strings.Contains(host, "kuaishou.com")) && (strings.Contains(path, "/short-video/") || strings.Contains(path, "/photo/"))
	case "zhihu":
		return (host == "www.zhihu.com" || host == "zhihu.com" || host == "zhuanlan.zhihu.com") && (strings.Contains(path, "/question/") || strings.Contains(path, "/p/") || strings.Contains(path, "/zvideo/"))
	case "xiaohongshu", "xhs":
		if host == "xhslink.com" || host == "www.xhslink.com" {
			return true
		}
		return (host == "www.xiaohongshu.com" || host == "xiaohongshu.com") && (strings.Contains(path, "/explore/") || strings.Contains(path, "/discovery/item/"))
	case "wechat", "weixin":
		return (host == "mp.weixin.qq.com" || host == "weixin.qq.com") && (path == "/s" || strings.HasPrefix(path, "/s/"))
	case "x", "twitter":
		return (host == "x.com" || host == "www.x.com" || strings.HasSuffix(host, ".twitter.com") || host == "twitter.com" || host == "www.twitter.com") && strings.Contains(path, "/status/")
	case "threads":
		return threadsplatform.IsThreadsHost(host) && strings.Contains(path, "/post/")
	case "instagram":
		return (host == "instagram.com" || host == "www.instagram.com") && (strings.Contains(path, "/p/") || strings.Contains(path, "/reel/"))
	case "web":
		if base == nil || strings.TrimSpace(base.Hostname()) == "" {
			return true
		}
		return host == strings.ToLower(base.Hostname())
	default:
		return true
	}
}

func contentTypeFor(platform, link string) string {
	if platform == "xiaohongshu" || platform == "xhs" || strings.Contains(link, "xiaohongshu.com/explore/") {
		return "note"
	}
	if platform == "douyin" || strings.Contains(link, "/video/") {
		if strings.Contains(link, "/note/") {
			return "note"
		}
		return "video"
	}
	if platform == "bilibili" || strings.Contains(link, "bilibili.com/video/") {
		return "video"
	}
	if platform == "kuaishou" || strings.Contains(link, "kuaishou.com/short-video/") || strings.Contains(link, "kuaishou.com/photo/") {
		return "video"
	}
	if platform == "weibo" || strings.Contains(link, "weibo.cn/detail/") {
		return "post"
	}
	if platform == "zhihu" || strings.Contains(link, "zhihu.com/question/") || strings.Contains(link, "zhihu.com/zvideo/") || strings.Contains(link, "zhuanlan.zhihu.com/p/") {
		if strings.Contains(link, "/zvideo/") {
			return "video"
		}
		if strings.Contains(link, "zhuanlan.zhihu.com/p/") {
			return "article"
		}
		return "answer"
	}
	if platform == "x" || platform == "twitter" || strings.Contains(link, "/status/") {
		return "x_post"
	}
	if platform == "threads" || strings.Contains(link, "threads.net/") || strings.Contains(link, "threads.com/") {
		return "thread"
	}
	if platform == "instagram" || strings.Contains(link, "instagram.com/reel/") {
		if strings.Contains(link, "/reel/") {
			return "video"
		}
		return "instagram_post"
	}
	return "article"
}

func webWeiboPostPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	first := strings.ToLower(strings.TrimSpace(parts[0]))
	switch first {
	case "u", "profile", "login", "signup", "search", "tv", "p":
		return false
	}
	return true
}

func xUsernameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
		return parts[0]
	}
	return ""
}

func threadsUsernameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		return strings.TrimPrefix(parts[0], "@")
	}
	return ""
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123, time.RubyDate, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func hashKey(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func optionString(opts map[string]any, key string) string {
	if opts == nil {
		return ""
	}
	v, ok := opts[key]
	if !ok {
		return ""
	}
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func optionInt(opts map[string]any, key string) int {
	if opts == nil {
		return 0
	}
	v, ok := opts[key]
	if !ok {
		return 0
	}
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		return 0
	}
}

func sourceLimit(opts map[string]any, defaultLimit, maxLimit int) int {
	limit := optionInt(opts, "limit")
	if limit <= 0 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		return maxLimit
	}
	return limit
}

func shortText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func optionBool(opts map[string]any, key string) bool {
	v, _ := optionBoolValue(opts, key)
	return v
}

func optionBoolValue(opts map[string]any, key string) (bool, bool) {
	if opts == nil {
		return false, false
	}
	v, ok := opts[key]
	if !ok {
		return false, false
	}
	switch value := v.(type) {
	case bool:
		return value, true
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(value))
		return b, true
	default:
		return false, true
	}
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func stringSliceFromAny(v any) []string {
	switch value := v.(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(stringFromAny(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(stringFromAny(v)); s != "" {
			return []string{s}
		}
		return nil
	}
}

func redditSubredditFromURL(rawURL string) string {
	parts := redditPathParts(rawURL)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "r") {
		return parts[1]
	}
	return ""
}

func redditUsernameFromURL(rawURL string) string {
	parts := redditPathParts(rawURL)
	if len(parts) >= 2 && (strings.EqualFold(parts[0], "user") || strings.EqualFold(parts[0], "u")) {
		return parts[1]
	}
	return ""
}

func redditPathParts(rawURL string) []string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func latestRedditComment(meta map[string]any) reddit.RedditComment {
	if meta == nil {
		return reddit.RedditComment{}
	}
	comments, ok := meta["comments_tree"].([]reddit.RedditComment)
	if !ok || len(comments) == 0 {
		return reddit.RedditComment{}
	}
	var latest reddit.RedditComment
	var walk func([]reddit.RedditComment)
	walk = func(items []reddit.RedditComment) {
		for _, c := range items {
			if c.CreatedUTC > latest.CreatedUTC || latest.ID == "" {
				latest = c
			}
			walk(c.Replies)
		}
	}
	walk(comments)
	return latest
}

func commentAuthorOrPostAuthor(comment reddit.RedditComment, content *platform.Content) string {
	if comment.Author != "" {
		return comment.Author
	}
	if content != nil && content.Author != nil {
		return content.Author.Name
	}
	return ""
}

func timeFromUnix(ts int64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}
