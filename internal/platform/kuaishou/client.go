package kuaishou

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	kuaishouGraphQLTimeout = 10 * time.Second
	kuaishouGraphQLPath    = "/graphql"
)

var (
	kuaishouGraphQLBaseURL = "https://www.kuaishou.com"
	kuaishouHTTPClient     = http.DefaultClient
)

type graphQLRequest struct {
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
	Query         string         `json:"query"`
}

type graphQLResponse struct {
	Data   graphQLData     `json:"data"`
	Errors []graphQLError  `json:"errors"`
	Result any             `json:"result"`
	Raw    json.RawMessage `json:"-"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLData struct {
	VideoDetail      *videoDetail      `json:"visionVideoDetail"`
	ProfilePhotoList *profilePhotoList `json:"visionProfilePhotoList"`
}

type videoDetail struct {
	Status string      `json:"status"`
	Type   string      `json:"type"`
	Author ksAuthor    `json:"author"`
	Photo  ksPhoto     `json:"photo"`
	Tags   []ksTag     `json:"tags"`
	LLSID  string      `json:"llsid"`
	Raw    graphQLData `json:"-"`
}

type profilePhotoList struct {
	Result  any      `json:"result"`
	Feeds   []ksFeed `json:"feeds"`
	PCursor string   `json:"pcursor"`
}

type ksFeed struct {
	Status string   `json:"status"`
	Author ksAuthor `json:"author"`
	Photo  ksPhoto  `json:"photo"`
	Tags   []ksTag  `json:"tags"`
}

type ksAuthor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HeaderURL string `json:"headerUrl"`
}

type ksPhoto struct {
	ID            string         `json:"id"`
	Caption       string         `json:"caption"`
	OriginCaption string         `json:"originCaption"`
	Duration      any            `json:"duration"`
	LikeCount     any            `json:"likeCount"`
	RealLikeCount any            `json:"realLikeCount"`
	ViewCount     any            `json:"viewCount"`
	CommentCount  any            `json:"commentCount"`
	CoverURL      string         `json:"coverUrl"`
	PhotoURL      string         `json:"photoUrl"`
	PhotoH265URL  string         `json:"photoH265Url"`
	Timestamp     any            `json:"timestamp"`
	Manifest      map[string]any `json:"manifest"`
	VideoResource any            `json:"videoResource"`
}

type ksTag struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type cookieParam struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

type APIListItem struct {
	ID           string
	CanonicalURL string
	Title        string
	ContentType  string
	PublishedAt  time.Time
	Raw          map[string]any
}

func FetchVideoViaAPI(ctx context.Context, norm *platform.NormalizedURL, in platform.ReadInput) (*platform.Content, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" || norm.ContentKind == "user" {
		attempt := kuaishouAPIFallbackAttempt(0, "missing_video_id", "missing Kuaishou video id")
		return nil, attempt, 0, attempt.Error()
	}
	start := time.Now()
	var parsed graphQLResponse
	attempt, err := postGraphQL(ctx, "visionVideoDetail", map[string]any{"photoId": norm.PlatformID, "page": "search"}, kuaishouVideoDetailQuery, in, &parsed)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, attempt, latency, err
	}
	if parsed.Data.VideoDetail == nil || strings.TrimSpace(parsed.Data.VideoDetail.Photo.ID) == "" {
		attempt := kuaishouAPIFallbackAttempt(200, "empty_payload", "Kuaishou API response missing video detail")
		return nil, attempt, latency, attempt.Error()
	}
	content := mapAPIVideo(in, norm, parsed.Data.VideoDetail)
	return content, attempt, latency, nil
}

func ListUserVideosViaAPI(ctx context.Context, norm *platform.NormalizedURL, in platform.ReadInput, limit int) ([]APIListItem, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" || norm.ContentKind != "user" {
		attempt := kuaishouAPIFallbackAttempt(0, "missing_user_id", "missing Kuaishou user id")
		return nil, attempt, 0, attempt.Error()
	}
	start := time.Now()
	var parsed graphQLResponse
	attempt, err := postGraphQL(ctx, "visionProfilePhotoList", map[string]any{"userId": norm.PlatformID, "pcursor": "", "page": "profile"}, kuaishouProfilePhotoListQuery, in, &parsed)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, attempt, latency, err
	}
	list := parsed.Data.ProfilePhotoList
	if list == nil || len(list.Feeds) == 0 {
		attempt := kuaishouAPIFallbackAttempt(200, "empty_payload", "Kuaishou API response missing profile videos")
		return nil, attempt, latency, attempt.Error()
	}
	if limit <= 0 {
		limit = 20
	}
	items := make([]APIListItem, 0, minInt(limit, len(list.Feeds)))
	for _, feed := range list.Feeds {
		id := strings.TrimSpace(feed.Photo.ID)
		if id == "" {
			continue
		}
		canonical := "https://www.kuaishou.com/short-video/" + id
		items = append(items, APIListItem{
			ID:           "kuaishou:" + id,
			CanonicalURL: canonical,
			Title:        firstNonEmpty(feed.Photo.Caption, feed.Photo.OriginCaption, "快手视频 "+id),
			ContentType:  ContentType,
			PublishedAt:  timestampTime(feed.Photo.Timestamp),
			Raw: map[string]any{
				"platform_id": id,
				"source":      "api",
				"author_id":   feed.Author.ID,
				"author_name": feed.Author.Name,
			},
		})
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		attempt := kuaishouAPIFallbackAttempt(200, "empty_payload", "Kuaishou API response contained no usable videos")
		return nil, attempt, latency, attempt.Error()
	}
	return items, attempt, latency, nil
}

func postGraphQL(ctx context.Context, operationName string, variables map[string]any, query string, in platform.ReadInput, out *graphQLResponse) (*platform.APIAttempt, error) {
	apiCtx, cancel := context.WithTimeout(ctx, kuaishouGraphQLTimeout)
	defer cancel()
	body, err := json.Marshal(graphQLRequest{OperationName: operationName, Variables: variables, Query: query})
	if err != nil {
		attempt := kuaishouAPIFallbackAttempt(0, "request_build_failed", err.Error())
		return attempt, attempt.Error()
	}
	endpoint := strings.TrimRight(kuaishouGraphQLBaseURL, "/") + kuaishouGraphQLPath
	req, err := http.NewRequestWithContext(apiCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		attempt := kuaishouAPIFallbackAttempt(0, "request_build_failed", err.Error())
		return attempt, attempt.Error()
	}
	applyAPIHeaders(req, in)
	resp, err := kuaishouHTTPClient.Do(req)
	if err != nil {
		attempt := kuaishouAPIFallbackAttempt(0, "request_failed", err.Error())
		return attempt, attempt.Error()
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		attempt := kuaishouAPIFallbackAttempt(resp.StatusCode, "read_failed", readErr.Error())
		return attempt, attempt.Error()
	}
	if isRiskControlled(resp, data) {
		meta := map[string]any{
			"platform":        PlatformName,
			"api_http_status": resp.StatusCode,
			"api_endpoint":    endpoint,
			"api_fetch_mode":  "graphql",
			"risk_control":    true,
		}
		return nil, platform.NewReadError(platform.ErrCaptchaRequired, "快手 GraphQL 触发风控，需要登录态或安全验证", PlatformName, meta)
	}
	if err := json.Unmarshal(data, out); err != nil {
		attempt := kuaishouAPIFallbackAttempt(resp.StatusCode, "decode_failed", "Kuaishou GraphQL response is not JSON")
		return attempt, attempt.Error()
	}
	out.Raw = data
	attempt := platform.ClassifyAPIResponse(PlatformName, resp.StatusCode, map[string]any{
		"data": len(data),
	}, func(m map[string]any) bool {
		return len(data) == 0
	})
	attempt.Meta["api_endpoint"] = endpoint
	attempt.Meta["api_fetch_mode"] = "graphql"
	if len(out.Errors) > 0 {
		attempt := kuaishouAPIFallbackAttempt(resp.StatusCode, "graphql_errors", graphQLErrorMessage(out.Errors))
		return attempt, attempt.Error()
	}
	if attempt.OK {
		return attempt, nil
	}
	return attempt, attempt.Error()
}

func applyAPIHeaders(req *http.Request, in platform.ReadInput) {
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.kuaishou.com")
	req.Header.Set("Referer", firstNonEmpty(in.URL, "https://www.kuaishou.com/"))
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl")
	if cookie := loadCookieHeader(firstNonEmpty(in.UseCookieDomain, CookieDomain)); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

func loadCookieHeader(domain string) string {
	dir := config.Get().LocalBrowser.CookieStoreDir
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, sanitizeCookieDomain(domain)+".json"))
	if err != nil {
		return ""
	}
	var cookies []cookieParam
	if err := json.Unmarshal(data, &cookies); err != nil {
		return ""
	}
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func sanitizeCookieDomain(domain string) string {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return CookieDomain
	}
	return domain
}

func isRiskControlled(resp *http.Response, body []byte) bool {
	if resp == nil {
		return false
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Intercept-Result")), "risk-control") {
		return true
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		return int64FromAny(parsed["result"]) == 2 && strings.TrimSpace(stringFromAny(parsed["request_id"])) != ""
	}
	text := strings.ToLower(strings.TrimSpace(string(body)))
	return strings.Contains(text, `"result":2`) && strings.Contains(text, "request_id")
}

func graphQLErrorMessage(errors []graphQLError) string {
	var parts []string
	for _, e := range errors {
		if strings.TrimSpace(e.Message) != "" {
			parts = append(parts, strings.TrimSpace(e.Message))
		}
	}
	if len(parts) == 0 {
		return "Kuaishou GraphQL returned errors"
	}
	return strings.Join(parts, "; ")
}

func mapAPIVideo(in platform.ReadInput, norm *platform.NormalizedURL, detail *videoDetail) *platform.Content {
	photo := detail.Photo
	id := firstNonEmpty(photo.ID, norm.PlatformID)
	canonical := norm.CanonicalURL
	if canonical == "" && id != "" {
		canonical = "https://www.kuaishou.com/short-video/" + id
	}
	meta := map[string]any{
		"extraction_source": "api",
		"platform_id":       id,
		"video_id":          id,
		"api_fetch_mode":    "graphql",
		"api_status":        detail.Status,
		"llsid":             detail.LLSID,
	}
	title := firstNonEmpty(photo.Caption, photo.OriginCaption, "快手视频 "+id)
	text := buildSummaryInput(photo, detail.Author)
	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            title,
		Text:             text,
		Markdown:         text,
		PublishedAt:      timestampString(photo.Timestamp),
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
		Author: &platform.Author{
			ID:        strings.TrimSpace(detail.Author.ID),
			Name:      strings.TrimSpace(detail.Author.Name),
			AvatarURL: strings.TrimSpace(detail.Author.HeaderURL),
		},
		Engagement: &platform.Engagement{
			Likes:    intFromAny(firstNonNil(photo.RealLikeCount, photo.LikeCount)),
			Comments: intFromAny(photo.CommentCount),
		},
		Tags: tagNames(detail.Tags),
	}
	if c.Author.ID == "" && c.Author.Name == "" && c.Author.AvatarURL == "" {
		c.Author = nil
	}
	if c.Engagement.Likes == 0 && c.Engagement.Comments == 0 {
		c.Engagement = nil
	}
	if in.IncludeMedia {
		c.Media = mediaFromAPIPhoto(photo)
	}
	return c
}

func buildSummaryInput(photo ksPhoto, author ksAuthor) string {
	var parts []string
	if caption := firstNonEmpty(photo.Caption, photo.OriginCaption); caption != "" {
		parts = append(parts, "文案: "+caption)
	}
	if authorName := strings.TrimSpace(author.Name); authorName != "" {
		parts = append(parts, "作者: "+authorName)
	}
	if views := intFromAny(photo.ViewCount); views > 0 {
		parts = append(parts, fmt.Sprintf("播放: %d", views))
	}
	if likes := intFromAny(firstNonNil(photo.RealLikeCount, photo.LikeCount)); likes > 0 {
		parts = append(parts, fmt.Sprintf("点赞: %d", likes))
	}
	if comments := intFromAny(photo.CommentCount); comments > 0 {
		parts = append(parts, fmt.Sprintf("评论: %d", comments))
	}
	return strings.Join(parts, "\n")
}

func mediaFromAPIPhoto(photo ksPhoto) []platform.MediaItem {
	var media []platform.MediaItem
	if u := strings.TrimSpace(photo.CoverURL); u != "" {
		media = append(media, platform.MediaItem{Type: "image", URL: u})
	}
	for _, u := range []string{photo.PhotoURL, photo.PhotoH265URL} {
		if strings.TrimSpace(u) != "" {
			media = append(media, platform.MediaItem{Type: "video", URL: strings.TrimSpace(u)})
		}
	}
	if u := firstManifestURL(photo.Manifest); u != "" {
		media = append(media, platform.MediaItem{Type: "video", URL: u})
	}
	return media
}

func firstManifestURL(manifest map[string]any) string {
	sets, _ := manifest["adaptationSet"].([]any)
	for _, set := range sets {
		m, _ := set.(map[string]any)
		reps, _ := m["representation"].([]any)
		for _, rep := range reps {
			rm, _ := rep.(map[string]any)
			for _, key := range []string{"url", "backupUrl"} {
				if u := platform.MetaString(rm, key); u != "" {
					return u
				}
			}
		}
	}
	return ""
}

func timestampString(v any) string {
	t := timestampTime(v)
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func timestampTime(v any) time.Time {
	n := int64FromAny(v)
	if n <= 0 {
		return time.Time{}
	}
	if n > 1_000_000_000_000 {
		n /= 1000
	}
	return time.Unix(n, 0)
}

func intFromAny(v any) int {
	return int(int64FromAny(v))
}

func int64FromAny(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(t), ",", ""), 10, 64)
		return n
	default:
		return 0
	}
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil && fmt.Sprint(v) != "" {
			return v
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func tagNames(tags []ksTag) []string {
	var out []string
	for _, tag := range tags {
		if strings.TrimSpace(tag.Name) != "" {
			out = append(out, strings.TrimSpace(tag.Name))
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func kuaishouAPIFallbackAttempt(status int, code, msg string) *platform.APIAttempt {
	return &platform.APIAttempt{
		FallbackBrowser: true,
		ErrorCode:       code,
		Message:         msg,
		HTTPStatus:      status,
		Meta: map[string]any{
			"platform":        PlatformName,
			"api_http_status": status,
			"api_error_code":  code,
			"api_fetch_mode":  "graphql",
		},
	}
}

const kuaishouVideoDetailQuery = `query visionVideoDetail($photoId: String, $type: String, $page: String, $webPageArea: String) {
  visionVideoDetail(photoId: $photoId, type: $type, page: $page, webPageArea: $webPageArea) {
    status
    type
    author { id name headerUrl }
    photo {
      id duration caption originCaption likeCount realLikeCount coverUrl photoUrl photoH265Url timestamp viewCount commentCount
      manifest { adaptationSet { representation { url backupUrl } } }
    }
    tags { type name }
    llsid
  }
}`

const kuaishouProfilePhotoListQuery = `query visionProfilePhotoList($pcursor: String, $userId: String, $page: String, $webPageArea: String) {
  visionProfilePhotoList(pcursor: $pcursor, userId: $userId, page: $page, webPageArea: $webPageArea) {
    result
    feeds {
      status
      author { id name headerUrl }
      photo { id caption originCaption likeCount realLikeCount coverUrl photoUrl photoH265Url timestamp viewCount commentCount }
      tags { type name }
    }
    pcursor
  }
}`
