package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

const bilibiliAPITimeout = 5 * time.Second

var (
	bilibiliAPIBaseURL = "https://api.bilibili.com"
	bilibiliHTTPClient = http.DefaultClient
)

type viewResponse struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    viewData `json:"data"`
	TTL     int      `json:"ttl"`
}

type viewData struct {
	BVID      string     `json:"bvid"`
	AID       int64      `json:"aid"`
	CID       int64      `json:"cid"`
	Title     string     `json:"title"`
	Desc      string     `json:"desc"`
	Pic       string     `json:"pic"`
	PubDate   int64      `json:"pubdate"`
	Owner     viewOwner  `json:"owner"`
	Stat      viewStat   `json:"stat"`
	Pages     []viewPage `json:"pages"`
	Dimension any        `json:"dimension"`
}

type viewOwner struct {
	MID  int64  `json:"mid"`
	Name string `json:"name"`
	Face string `json:"face"`
}

type viewStat struct {
	View     int `json:"view"`
	Danmaku  int `json:"danmaku"`
	Reply    int `json:"reply"`
	Favorite int `json:"favorite"`
	Coin     int `json:"coin"`
	Share    int `json:"share"`
	Like     int `json:"like"`
}

type viewPage struct {
	CID      int64  `json:"cid"`
	Page     int    `json:"page"`
	Part     string `json:"part"`
	Duration int    `json:"duration"`
}

func FetchVideoViaAPI(ctx context.Context, norm *platform.NormalizedURL) (*viewResponse, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" || norm.ContentKind == "user" {
		attempt := bilibiliAPIFallbackAttempt(0, "", "missing_video_id", "missing Bilibili video id")
		return nil, attempt, 0, attempt.Error()
	}
	apiCtx, cancel := context.WithTimeout(ctx, bilibiliAPITimeout)
	defer cancel()

	endpoint, err := bilibiliViewURL(norm)
	if err != nil {
		attempt := bilibiliAPIFallbackAttempt(0, "", "endpoint_build_failed", err.Error())
		return nil, attempt, 0, attempt.Error()
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(apiCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		attempt := bilibiliAPIFallbackAttempt(0, endpoint, "request_build_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", firstNonEmpty(norm.CanonicalURL, "https://www.bilibili.com/"))
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl")

	resp, err := bilibiliHTTPClient.Do(req)
	if err != nil {
		attempt := bilibiliAPIFallbackAttempt(0, endpoint, "request_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		attempt := bilibiliAPIFallbackAttempt(resp.StatusCode, endpoint, "read_failed", readErr.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	var parsed viewResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		attempt := bilibiliAPIFallbackAttempt(resp.StatusCode, endpoint, "decode_failed", "Bilibili API response is not JSON")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	attempt := platform.ClassifyAPIResponse(PlatformName, resp.StatusCode, map[string]any{
		"code":    parsed.Code,
		"message": parsed.Message,
		"data":    parsed.Data.Title,
	}, func(m map[string]any) bool {
		return strings.TrimSpace(fmt.Sprint(m["data"])) == ""
	})
	attempt.Meta["api_endpoint"] = endpoint
	attempt.Meta["api_fetch_mode"] = "http"
	attempt.Meta["api_signature_mode"] = "none"
	if attempt.OK {
		return &parsed, attempt, time.Since(start).Milliseconds(), nil
	}
	return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
}

func bilibiliViewURL(norm *platform.NormalizedURL) (string, error) {
	u, err := url.Parse(strings.TrimRight(bilibiliAPIBaseURL, "/") + "/x/web-interface/view")
	if err != nil {
		return "", err
	}
	q := u.Query()
	if strings.EqualFold(norm.ContentKind, "av") {
		q.Set("aid", norm.PlatformID)
	} else if strings.HasPrefix(strings.ToUpper(norm.PlatformID), "BV") {
		q.Set("bvid", norm.PlatformID)
	} else {
		q.Set("aid", norm.PlatformID)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func bilibiliAPIFallbackAttempt(status int, endpoint, code, msg string) *platform.APIAttempt {
	meta := map[string]any{
		"platform":        PlatformName,
		"api_endpoint":    endpoint,
		"api_fetch_mode":  "http",
		"api_error_code":  code,
		"api_http_status": status,
	}
	return &platform.APIAttempt{FallbackBrowser: true, ErrorCode: code, Message: msg, HTTPStatus: status, Meta: meta}
}
