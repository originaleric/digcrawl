package weibo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

const weiboAPITimeout = 5 * time.Second

var (
	weiboAPIBaseURL = "https://m.weibo.cn"
	weiboHTTPClient = http.DefaultClient
)

type statusResponse struct {
	OK      int    `json:"ok"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
	statusPayload
}

type statusPayload struct {
	ID            string          `json:"id"`
	MID           string          `json:"mid"`
	Text          string          `json:"text"`
	TextRaw       string          `json:"text_raw"`
	CreatedAt     string          `json:"created_at"`
	Source        string          `json:"source"`
	User          statusUser      `json:"user"`
	Pics          []statusPic     `json:"pics"`
	PageInfo      statusPageInfo  `json:"page_info"`
	Retweeted     *statusPayload  `json:"retweeted_status"`
	RepostsCount  int             `json:"reposts_count"`
	CommentsCount int             `json:"comments_count"`
	Attitudes     int             `json:"attitudes_count"`
	Raw           json.RawMessage `json:"-"`
}

type statusUser struct {
	ID              int64  `json:"id"`
	ScreenName      string `json:"screen_name"`
	ProfileImageURL string `json:"profile_image_url"`
	ProfileURL      string `json:"profile_url"`
}

type statusPic struct {
	URL      string `json:"url"`
	Large    any    `json:"large"`
	Geo      any    `json:"geo"`
	PID      string `json:"pid"`
	Original string `json:"original"`
}

type statusPageInfo struct {
	Type     string `json:"type"`
	PageURL  string `json:"page_url"`
	MediaURL string `json:"media_url"`
	PagePic  string `json:"page_pic"`
	Title    string `json:"page_title"`
}

func FetchPostViaAPI(ctx context.Context, norm *platform.NormalizedURL) (*statusResponse, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" || norm.ContentKind == "user" {
		attempt := weiboAPIFallbackAttempt(0, "", "missing_post_id", "missing Weibo post id")
		return nil, attempt, 0, attempt.Error()
	}
	apiCtx, cancel := context.WithTimeout(ctx, weiboAPITimeout)
	defer cancel()
	endpoint := strings.TrimRight(weiboAPIBaseURL, "/") + "/statuses/show?id=" + url.QueryEscape(norm.PlatformID)
	start := time.Now()
	req, err := http.NewRequestWithContext(apiCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		attempt := weiboAPIFallbackAttempt(0, endpoint, "request_build_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", firstNonEmpty(norm.CanonicalURL, "https://m.weibo.cn/"))
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl")

	resp, err := weiboHTTPClient.Do(req)
	if err != nil {
		attempt := weiboAPIFallbackAttempt(0, endpoint, "request_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		attempt := weiboAPIFallbackAttempt(resp.StatusCode, endpoint, "read_failed", readErr.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	var parsed statusResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		attempt := weiboAPIFallbackAttempt(resp.StatusCode, endpoint, "decode_failed", "Weibo API response is not JSON")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	attempt := platform.ClassifyAPIResponse(PlatformName, resp.StatusCode, map[string]any{
		"code":    0,
		"success": parsed.OK == 1,
		"ok":      parsed.OK,
		"message": firstNonEmpty(parsed.Msg, parsed.Message),
		"data":    firstNonEmpty(parsed.TextRaw, parsed.Text, parsed.ID, parsed.MID),
	}, func(m map[string]any) bool {
		return strings.TrimSpace(firstNonEmpty(platform.MetaString(m, "data"), parsed.ID, parsed.MID)) == ""
	})
	attempt.Meta["api_endpoint"] = endpoint
	attempt.Meta["api_fetch_mode"] = "http"
	attempt.Meta["api_signature_mode"] = "none"
	if attempt.OK {
		return &parsed, attempt, time.Since(start).Milliseconds(), nil
	}
	return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
}

func weiboAPIFallbackAttempt(status int, endpoint, code, msg string) *platform.APIAttempt {
	meta := map[string]any{
		"platform":        PlatformName,
		"api_endpoint":    endpoint,
		"api_fetch_mode":  "http",
		"api_error_code":  code,
		"api_http_status": status,
	}
	return &platform.APIAttempt{FallbackBrowser: true, ErrorCode: code, Message: msg, HTTPStatus: status, Meta: meta}
}
