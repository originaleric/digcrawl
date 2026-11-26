package wechatchannels

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/testutil"
)

type yuanbaoRoundTrip func(*http.Request) (*http.Response, error)

func (f yuanbaoRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func yuanbaoResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

const (
	testShortLink = "https://weixin.qq.com/sph/AauAggqePs"
	testSignedURL = "https://finder.video.qq.com/file?proof=opaque%2B%2F&x=1&x=2"
)

func TestYuanbaoResolveAndDownloadMP4(t *testing.T) {
	seen := 0
	client := &http.Client{Transport: yuanbaoRoundTrip(func(r *http.Request) (*http.Response, error) {
		seen++
		var body map[string]any
		switch seen {
		case 1:
			if r.Method != "POST" || r.URL.String() != browser.YuanbaoParseURL || r.Header.Get("Cookie") != "hy_user=fake; hy_token=fake" {
				t.Fatalf("unexpected Yuanbao request")
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["url"] != testShortLink || body["type"] != "video_channel_url" || body["scene"] != float64(1) {
				t.Fatalf("unexpected parse body: %+v, %v", body, err)
			}
			return yuanbaoResponse(200, `{"code":0,"data":{"playable_url":"https://channels.weixin.qq.com/finder-preview/pages/feed?token=test%2Btoken&eid=fake-id","desc":"fallback","author":"fallback author","cover_url":"https://wx.qpic.cn/cover.jpg"}}`), nil
		case 2:
			if r.Method != "POST" || r.URL.Host != previewHost || r.URL.Path != feedAPIPath || r.URL.Query().Get("_rid") == "" || r.URL.Query().Get("_pageUrl") != previewOrigin+feedPagePath || r.Header.Get("Cookie") != "" {
				t.Fatal("unexpected video preview request")
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["exportId"] != "fake-id" {
				t.Fatalf("unexpected preview body: %+v, %v", body, err)
			}
			if base, ok := body["baseReq"].(map[string]any); !ok || base["generalToken"] != "test+token" {
				t.Fatal("preview token did not survive query decoding")
			}
			return yuanbaoResponse(200, `{"errCode":0,"data":{"feedInfo":{"mediaType":4,"description":"short video","h264VideoInfo":{"videoUrl":"`+testSignedURL+`"},"videoUrl":"https://finder.video.qq.com/fallback"},"authorInfo":{"nickname":"video author"}}}`), nil
		case 3, 4:
			if r.Method != "GET" || r.URL.String() != testSignedURL || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
				t.Fatal("media request failed to preserve the signed query or leaked request context")
			}
			resp := yuanbaoResponse(200, testutil.MP4)
			resp.Header.Set("Content-Type", "video/mp4")
			return resp, nil
		default:
			t.Fatal("unexpected extra request")
			return nil, errors.New("unexpected request")
		}
	})}
	source := platform.ReadInput{URL: testShortLink + "?unused=tracking#fragment", IncludeMedia: true}
	content, video, err := resolveShareVideo(t.Context(), source, client, func() (string, error) {
		return "hy_user=fake; hy_token=fake", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.CanonicalURL != testShortLink || content.Title != "short video" || content.Author == nil || content.Author.Name != "video author" {
		t.Fatalf("unexpected result: %+v", content)
	}
	if len(content.Media) != 1 || content.Media[0].Type != "image" || content.Media[0].URL != "https://wx.qpic.cn/cover.jpg" {
		t.Fatalf("expected only the cover in provider media: %+v", content.Media)
	}
	output, err := json.Marshal(content.ToMap())
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"finder.video.qq.com", "hy_token", "fake-id", "test%2Btoken", "unused=tracking"} {
		if strings.Contains(string(output), private) {
			t.Fatalf("private data present in result: %s", private)
		}
	}
	path, cleanup, err := video.download(t.Context(), client, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != testutil.MP4 || filepath.Base(path) != "video.mp4" || seen != 3 {
		t.Fatalf("unexpected temporary download: requests=%d, err=%v", seen, err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected temporary cleanup: %v", err)
	}
	retainedRoot := t.TempDir()
	retained, cleanupRetained, err := video.download(t.Context(), client, 1024, retainedRoot)
	if err != nil {
		t.Fatal(err)
	}
	cleanupRetained()
	got, err = os.ReadFile(retained)
	if err != nil || string(got) != testutil.MP4 || filepath.Dir(filepath.Dir(retained)) != retainedRoot || seen != 4 {
		t.Fatalf("expected complete retained video beneath %s: path=%q err=%v", retainedRoot, retained, err)
	}
}

func TestYuanbaoLoginErrorWithEmptyLocalProfile(t *testing.T) {
	_, _, err := ResolveShareVideo(t.Context(), platform.ReadInput{URL: testShortLink}, &config.Config{LocalBrowser: config.LocalBrowserConfig{CookieStoreDir: t.TempDir()}})
	requireYuanbaoError(t, err, platform.ErrLoginRequired)
	var readErr *platform.ReadError
	_ = errors.As(err, &readErr)
	if readErr.Meta["login_platform"] != "wechat_channels" || readErr.Meta["login_url"] != browser.YuanbaoOrigin {
		t.Fatalf("incomplete login action: %+v", readErr.Meta)
	}
}

func TestYuanbaoLoginActionUsesRequestProfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	profile := filepath.Join(dir, "user's profile.yaml")
	cfg := &config.Config{SourcePath: profile, LocalBrowser: config.LocalBrowserConfig{CookieStoreDir: "cookies"}}
	_, _, readErr := ResolveShareVideo(t.Context(), platform.ReadInput{URL: testShortLink}, cfg)
	for _, err := range []error{readErr, CheckYuanbaoSession(cfg)} {
		requireYuanbaoError(t, err, platform.ErrLoginRequired)
		var login *platform.ReadError
		errors.As(err, &login)
		command, _ := login.Meta["login_command"].(string)
		if login.Meta["login_config_path"] != profile || login.Meta["login_working_directory"] != dir ||
			!strings.Contains(command, `--config '`+dir+`/user'"'"'s profile.yaml'`) || !strings.HasPrefix(command, "(cd ") {
			t.Fatalf("login action is not bound to the request profile and working directory: %+v", login.Meta)
		}
	}
}

func TestYuanbaoUnavailableResponsesDoNotFollowRedirectsOrLeakSecrets(t *testing.T) {
	const fakeSecret = "private-test-marker"
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"expired", `{"code":-1,"msg":"请重新登录 ` + fakeSecret + `"}`, platform.ErrLoginRequired, 200},
		{"denied", fakeSecret, platform.ErrLoginRequired, 401},
		{"captcha", `{"code":-1,"msg":"请完成安全验证 ` + fakeSecret + `"}`, platform.ErrCaptchaRequired, 200},
		{"redirect", "", platform.ErrPartialContent, http.StatusTemporaryRedirect},
		{"untrusted preview", `{"code":0,"data":{"playable_url":"https://example.com/finder-preview/pages/feed?token=` + fakeSecret + `&eid=id"}}`, platform.ErrPartialContent, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: yuanbaoRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls != 1 || r.URL.String() != browser.YuanbaoParseURL {
					t.Fatal("authenticated request escaped the expected origin")
				}
				resp := yuanbaoResponse(tc.status, tc.body)
				resp.Header.Set("Location", "https://example.com/redirect?marker="+fakeSecret)
				return resp, nil
			})}
			_, _, err := resolveShareVideo(t.Context(), platform.ReadInput{URL: testShortLink}, client, func() (string, error) {
				return "hy_user=fake; hy_token=" + fakeSecret, nil
			})
			requireYuanbaoError(t, err, tc.code)
			if strings.Contains(err.Error(), fakeSecret) {
				t.Fatal("upstream message leaked")
			}
		})
	}
}

func TestYuanbaoDownloadCleansFailedAcquisitions(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentRange string
		size                     int64
		status                   int
	}{
		{name: "size limit", body: testutil.MP4, size: 12, status: http.StatusOK},
		{name: "file type box only", body: testutil.MP4[:28], size: 1024, status: http.StatusOK},
		{name: "truncated media data", body: testutil.MP4[:len(testutil.MP4)-1], size: 1024, status: http.StatusOK},
		{name: "invalid file", body: "<html>fake</html>", size: 1024, status: http.StatusOK},
		{name: "foreign redirect", size: 1024, status: http.StatusFound},
		{name: "partial video", body: testutil.MP4, size: 1024, status: http.StatusPartialContent, contentRange: "bytes 0-23/500000"},
		{name: "range with 200", body: testutil.MP4, size: 1024, status: http.StatusOK, contentRange: "bytes 0-23/500000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(key, dir)
			}
			client := &http.Client{Transport: yuanbaoRoundTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != testSignedURL {
					t.Fatal("unexpected download target")
				}
				resp := yuanbaoResponse(tc.status, tc.body)
				if tc.status == http.StatusFound {
					resp.Header.Set("Location", "https://example.com/media")
				}
				if tc.contentRange != "" {
					resp.Header.Set("Content-Range", tc.contentRange)
				}
				return resp, nil
			})}
			video := &ShareVideo{mediaURL: testSignedURL}
			_, _, err := video.download(t.Context(), client, tc.size, "")
			requireYuanbaoError(t, err, platform.ErrPartialContent)
			if strings.Contains(err.Error(), "opaque") {
				t.Fatal("signed media query leaked")
			}
			paths, err := os.ReadDir(dir)
			if err != nil || len(paths) != 0 {
				t.Fatalf("failed download left files: %v, %v", paths, err)
			}
		})
	}
}

func requireYuanbaoError(t *testing.T, err error, code string) {
	t.Helper()
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
