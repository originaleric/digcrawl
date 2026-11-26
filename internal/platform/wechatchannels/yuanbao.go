package wechatchannels

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/media"
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	previewOrigin = "https://" + previewHost
	feedPagePath  = "/finder-preview/pages/feed"
	feedAPIPath   = "/finder-preview/api/feed/get_feed_info"
	jsonMaxBytes  = 1 << 20
	// ShareVideoResolveTimeout allows one request each to Yuanbao and WeChat.
	ShareVideoResolveTimeout = 40 * time.Second
)

type yuanbaoParseResult struct {
	Code *int   `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PlayableURL string `json:"playable_url"`
		Description string `json:"desc"`
		Author      string `json:"author"`
		CoverURL    string `json:"cover_url"`
	} `json:"data"`
}

type feedVideo struct {
	URL string `json:"videoUrl"`
}

type previewFeedResult struct {
	Code int `json:"errCode"`
	Data struct {
		Feed *struct {
			Type        int       `json:"mediaType"`
			Description string    `json:"description"`
			URL         string    `json:"videoUrl"`
			H264        feedVideo `json:"h264VideoInfo"`
			H265        feedVideo `json:"h265VideoInfo"`
		} `json:"feedInfo"`
		Author struct {
			Nickname string `json:"nickname"`
		} `json:"authorInfo"`
		Error struct {
			Type    int    `json:"type"`
			Title   string `json:"title"`
			Content string `json:"content"`
		} `json:"errMsg"`
	} `json:"data"`
}

// ShareVideo owns the signed media URL for one short-link request. The URL
// stays private to the downloader and never becomes a Content media item.
type ShareVideo struct {
	mediaURL string
}

// ResolveShareVideo resolves a normal video using the locally authenticated
// Yuanbao session and the WeChat Channels preview endpoint.
func ResolveShareVideo(ctx context.Context, in platform.ReadInput, cfg *config.Config) (*platform.Content, *ShareVideo, error) {
	client := media.NewSafeHTTPClient(ShareVideoResolveTimeout / 2)
	defer client.CloseIdleConnections()
	content, video, err := resolveShareVideo(ctx, in, client, func() (string, error) {
		return browser.YuanbaoCookieHeader(cfg.LocalBrowser)
	})
	return content, video, bindYuanbaoLoginProfile(err, cfg)
}

// CheckYuanbaoSession checks whether a locally saved session can be used.
func CheckYuanbaoSession(cfg *config.Config) error {
	_, err := browser.YuanbaoCookieHeader(cfg.LocalBrowser)
	return bindYuanbaoLoginProfile(yuanbaoSessionError(err), cfg)
}

func yuanbaoSessionError(err error) error {
	var unavailable *browser.CookieSessionUnavailableError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &unavailable):
		return yuanbaoLoginRequired()
	default:
		return platform.NewReadError(platform.ErrPartialContent, "读取本地元宝会话失败", PlatformName, nil)
	}
}

func resolveShareVideo(ctx context.Context, in platform.ReadInput, client *http.Client, loadSession func() (string, error)) (*platform.Content, *ShareVideo, error) {
	if err := validateShareURL(in.URL); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.UseCookieDomain) != "" {
		return nil, nil, platform.NewReadError(platform.ErrScopeViolation, "视频号使用独立的元宝会话", PlatformName, nil)
	}
	start := time.Now()
	share, _ := url.Parse(strings.TrimSpace(in.URL))
	source := "https://" + shareHost + share.EscapedPath()
	cookie, err := loadSession()
	if err := yuanbaoSessionError(err); err != nil {
		return nil, nil, err
	}
	clientCopy := *client
	clientCopy.Jar = nil
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	var parsed yuanbaoParseResult
	err = requestVideoJSON(ctx, &clientCopy, browser.YuanbaoParseURL, browser.YuanbaoOrigin,
		browser.YuanbaoOrigin+"/", cookie, map[string]any{"type": "video_channel_url", "url": source, "scene": 1}, &parsed)
	if err != nil {
		return nil, nil, err
	}
	if parsed.Code == nil || *parsed.Code != 0 {
		return nil, nil, yuanbaoParseFailure(parsed)
	}
	playable, err := url.Parse(parsed.Data.PlayableURL)
	if err != nil || !isHTTPSURL(playable) || playable.Hostname() != previewHost ||
		!strings.HasPrefix(playable.EscapedPath(), "/finder-preview/pages/") {
		return nil, nil, platform.NewReadError(platform.ErrPartialContent, "元宝未返回有效的视频号预览", PlatformName, nil)
	}
	query, err := url.ParseQuery(playable.RawQuery)
	if err != nil || query.Get("token") == "" || query.Get("eid") == "" {
		return nil, nil, platform.NewReadError(platform.ErrPartialContent, "元宝未返回完整的视频号预览", PlatformName, nil)
	}

	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, platform.NewReadError(platform.ErrPartialContent, "无法创建视频号预览请求", PlatformName, nil)
	}
	params := url.Values{
		"_pageUrl": {previewOrigin + feedPagePath},
		"_rid":     {fmt.Sprintf("%x-%s", time.Now().Unix(), hex.EncodeToString(nonce[:]))},
	}
	var result previewFeedResult
	err = requestVideoJSON(ctx, &clientCopy, previewOrigin+feedAPIPath+"?"+params.Encode(), previewOrigin,
		playable.String(), "", map[string]any{
			"baseReq":  map[string]string{"generalToken": query.Get("token")},
			"exportId": query.Get("eid"),
		}, &result)
	if err != nil {
		return nil, nil, err
	}
	feed := result.Data.Feed
	infoError := result.Data.Error
	if result.Code != 0 || feed == nil || infoError.Type != 0 || infoError.Title != "" || infoError.Content != "" {
		return nil, nil, platform.NewReadError(platform.ErrPartialContent, "视频号预览不可用，请确认原视频仍可访问", PlatformName, nil)
	}
	if feed.Type != 4 {
		return nil, nil, platform.NewReadError(platform.ErrContentTypeMismatch, "此视频号链接不是普通短视频", PlatformName, nil)
	}
	var video *ShareVideo
	for _, candidate := range []string{feed.H264.URL, feed.URL, feed.H265.URL} {
		candidate = strings.TrimSpace(candidate)
		u, err := url.Parse(candidate)
		if err == nil && isChannelsMediaURL(u) {
			video = &ShareVideo{mediaURL: candidate}
			break
		}
	}
	if video == nil {
		return nil, nil, platform.NewReadError(platform.ErrPartialContent, "视频号未返回可下载的 MP4", PlatformName, nil)
	}

	title := firstText(feed.Description, parsed.Data.Description)
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        source,
		CanonicalURL:     source,
		Title:            firstText(title, "微信视频号"),
		Text:             title,
		Markdown:         title,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: map[string]any{"extraction_source": "yuanbao", "media_extraction_source": "yuanbao"},
	}
	if author := firstText(result.Data.Author.Nickname, parsed.Data.Author); author != "" {
		content.Author = &platform.Author{Name: author}
	}
	if in.IncludeMedia {
		content.Media = shareCover(parsed.Data.CoverURL)
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform: PlatformName, Channel: "api", ExtractionSource: "yuanbao",
		APILatencyMs: time.Since(start).Milliseconds(),
	})
	return content, video, nil
}

func requestVideoJSON(ctx context.Context, client *http.Client, endpoint, origin, referer, cookie string, payload map[string]any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return platform.NewReadError(platform.ErrPartialContent, "无法创建视频号请求", PlatformName, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36")
	if endpoint == browser.YuanbaoParseURL {
		req.Header.Set("X-Source", "web")
		req.Header.Set("Cookie", cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return platform.NewReadError(platform.ErrPartialContent, "视频号解析请求失败", PlatformName, nil)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return platform.NewReadError(platform.ErrRateLimited, "视频号解析请求过于频繁", PlatformName, nil)
	case endpoint == browser.YuanbaoParseURL && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden):
		return yuanbaoLoginRequired()
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		return platform.NewReadError(platform.ErrPartialContent, "视频号解析接口暂不可用", PlatformName, map[string]any{"http_status": resp.StatusCode})
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, jsonMaxBytes+1))
	if err != nil || len(data) > jsonMaxBytes || json.Unmarshal(data, target) != nil {
		return platform.NewReadError(platform.ErrPartialContent, "视频号解析接口返回了无效数据", PlatformName, nil)
	}
	return nil
}

func yuanbaoParseFailure(parsed yuanbaoParseResult) error {
	msg := strings.ToLower(parsed.Msg)
	switch {
	case strings.Contains(msg, "验证码") || strings.Contains(msg, "安全验证") || strings.Contains(msg, "captcha"):
		return platform.NewReadError(platform.ErrCaptchaRequired, "元宝要求安全验证，请打开元宝完成验证", PlatformName, nil)
	case strings.Contains(msg, "登录") || strings.Contains(msg, "login") || strings.Contains(msg, "unauthorized"),
		parsed.Code != nil && (*parsed.Code == http.StatusUnauthorized || *parsed.Code == http.StatusForbidden):
		return yuanbaoLoginRequired()
	case strings.Contains(msg, "频繁") || parsed.Code != nil && *parsed.Code == http.StatusTooManyRequests:
		return platform.NewReadError(platform.ErrRateLimited, "元宝解析请求过于频繁", PlatformName, nil)
	default:
		return platform.NewReadError(platform.ErrPartialContent, "元宝未能解析此视频号链接", PlatformName, nil)
	}
}

func yuanbaoLoginRequired() error {
	return platform.NewReadError(platform.ErrLoginRequired, "需要登录腾讯元宝。请使用当前 DigCrawl 配置登录后重试原链接。", PlatformName, map[string]any{
		"login_platform": PlatformName,
		"login_url":      browser.YuanbaoOrigin,
	})
}

func bindYuanbaoLoginProfile(err error, cfg *config.Config) error {
	var login *platform.ReadError
	if !errors.As(err, &login) || login.Code != platform.ErrLoginRequired || cfg.SourcePath == "" {
		return err
	}
	workingDir := ""
	login.Meta["login_config_path"] = cfg.SourcePath
	// Existing project profiles may resolve their session directory relative
	// to the running process. Preserve that directory when logging in.
	if !filepath.IsAbs(cfg.LocalBrowser.CookieStoreDir) {
		if dir, err := os.Getwd(); err == nil {
			login.Meta["login_working_directory"] = dir
			workingDir = dir
		}
	}
	command := yuanbaoLoginCommand(cfg.SourcePath, workingDir, runtime.GOOS)
	login.Meta["login_command"] = command
	login.Message = "需要登录腾讯元宝。请运行 " + command + "，登录后重试原链接。"
	return login
}

// yuanbaoLoginCommand preserves the request's session directory in the native
// shell without changing the user's working directory after login.
func yuanbaoLoginCommand(profile, workingDir, goos string) string {
	quote := func(value string) string {
		if goos == "windows" {
			return "'" + strings.ReplaceAll(value, "'", "''") + "'"
		}
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	command := "digcrawl browser login --platform wechat_channels --config " + quote(profile)
	if workingDir == "" {
		return command
	}
	if goos == "windows" {
		return "& { Push-Location -LiteralPath " + quote(workingDir) + " -ErrorAction Stop; try { " + command + " } finally { Pop-Location } }"
	}
	return "(cd " + quote(workingDir) + " && " + command + ")"
}

func isHTTPSURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443")
}

func isChannelsMediaURL(u *url.URL) bool {
	return isHTTPSURL(u) && isQQHost(u.Hostname())
}

func firstText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// Download acquires one bounded MP4. A configured retention root keeps the
// completed file. The returned cleanup releases a temporary download.
func (v *ShareVideo) Download(ctx context.Context, maxBytes int64, retainDir string) (string, func(), error) {
	client := media.NewSafeHTTPClient(0)
	defer client.CloseIdleConnections()
	return v.download(ctx, client, maxBytes, retainDir)
}

func (v *ShareVideo) download(ctx context.Context, client *http.Client, maxBytes int64, retainDir string) (string, func(), error) {
	fail := func(message string) (string, func(), error) {
		return "", nil, platform.NewReadError(platform.ErrPartialContent, message, PlatformName, nil)
	}
	if maxBytes <= 0 || maxBytes == (1<<63)-1 {
		return fail("视频号下载大小上限无效")
	}
	if retainDir != "" {
		if !filepath.IsAbs(retainDir) || os.MkdirAll(retainDir, 0o750) != nil {
			return fail("无法创建配置的视频号保留目录")
		}
	}
	dir, err := os.MkdirTemp(retainDir, "wechat-channels-")
	if err != nil {
		return fail("无法创建视频号下载目录")
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	complete := false
	defer func() {
		if !complete {
			cleanup()
		}
	}()

	clientCopy := *client
	clientCopy.Jar = nil
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !isChannelsMediaURL(req.URL) {
			return http.ErrUseLastResponse
		}
		req.Header.Del("Referer")
		return nil
	}
	tempPath := filepath.Join(dir, ".download")
	if _, err := media.DownloadFile(ctx, &clientCopy, v.mediaURL, tempPath, maxBytes, "", "video/mp4"); err != nil {
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		return fail("视频号 MP4 下载失败，请检查网络、视频可用性或下载大小上限")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "video.mp4")
	if err := os.Chmod(tempPath, 0o600); err != nil || os.Rename(tempPath, path) != nil {
		return fail("无法保存视频号 MP4")
	}
	complete = true
	if retainDir != "" {
		return path, func() {}, nil
	}
	return path, cleanup, nil
}
