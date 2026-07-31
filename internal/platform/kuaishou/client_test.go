package kuaishou

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchVideoViaAPIMapsGraphQLDetail(t *testing.T) {
	withKuaishouAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Origin"); got != "https://www.kuaishou.com" {
			t.Fatalf("unexpected origin: %q", got)
		}
		_, _ = w.Write([]byte(`{
			"data": {
				"visionVideoDetail": {
					"status": "1",
					"author": {"id": "u1", "name": "快手作者", "headerUrl": "https://p.example/avatar.jpg"},
					"photo": {
						"id": "3xf6abc",
						"caption": "快手视频文案",
						"likeCount": 12,
						"realLikeCount": 15,
						"viewCount": 100,
						"commentCount": 3,
						"coverUrl": "https://p.example/cover.jpg",
						"photoUrl": "https://v.example/video.mp4",
						"timestamp": 1785231600000
					},
					"tags": [{"name": "测试"}],
					"llsid": "lls1"
				}
			}
		}`))
	}), func() {
		content, attempt, _, err := FetchVideoViaAPI(t.Context(), &platform.NormalizedURL{
			OriginalURL:  "https://www.kuaishou.com/short-video/3xf6abc",
			PlatformID:   "3xf6abc",
			ContentKind:  ContentType,
			CanonicalURL: "https://www.kuaishou.com/short-video/3xf6abc",
		}, platform.ReadInput{URL: "https://www.kuaishou.com/short-video/3xf6abc", IncludeMedia: true})
		if err != nil {
			t.Fatal(err)
		}
		if attempt == nil || !attempt.OK {
			t.Fatalf("expected ok attempt, got %+v", attempt)
		}
		if content.Title != "快手视频文案" || content.Author == nil || content.Author.Name != "快手作者" {
			t.Fatalf("unexpected content: %+v", content)
		}
		if !strings.Contains(content.Text, "文案: 快手视频文案") || !strings.Contains(content.Text, "点赞: 15") {
			t.Fatalf("unexpected summary input: %q", content.Text)
		}
		if len(content.Media) != 2 {
			t.Fatalf("expected cover and video media, got %+v", content.Media)
		}
	})
}

func TestFetchVideoViaAPIClassifiesRiskControl(t *testing.T) {
	withKuaishouAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Intercept-Result", "risk-control;2")
		_, _ = w.Write([]byte(`{"result":2,"request_id":"rid"}`))
	}), func() {
		_, _, _, err := FetchVideoViaAPI(t.Context(), &platform.NormalizedURL{
			PlatformID:   "3xf6abc",
			ContentKind:  ContentType,
			CanonicalURL: "https://www.kuaishou.com/short-video/3xf6abc",
		}, platform.ReadInput{URL: "https://www.kuaishou.com/short-video/3xf6abc"})
		var re *platform.ReadError
		if !errors.As(err, &re) {
			t.Fatalf("expected ReadError, got %v", err)
		}
		if re.Code != platform.ErrCaptchaRequired {
			t.Fatalf("expected CAPTCHA_REQUIRED, got %+v", re)
		}
	})
}

func TestFetchVideoViaAPIClassifiesSpacedRiskControlJSON(t *testing.T) {
	withKuaishouAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result": 2, "request_id": "rid"}`))
	}), func() {
		_, _, _, err := FetchVideoViaAPI(t.Context(), &platform.NormalizedURL{
			PlatformID:   "3xf6abc",
			ContentKind:  ContentType,
			CanonicalURL: "https://www.kuaishou.com/short-video/3xf6abc",
		}, platform.ReadInput{URL: "https://www.kuaishou.com/short-video/3xf6abc"})
		var re *platform.ReadError
		if !errors.As(err, &re) {
			t.Fatalf("expected ReadError, got %v", err)
		}
		if re.Code != platform.ErrCaptchaRequired {
			t.Fatalf("expected CAPTCHA_REQUIRED, got %+v", re)
		}
	})
}

func TestReadVideoStopsOnAPIRiskControl(t *testing.T) {
	withKuaishouAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":2,"request_id":"rid"}`))
	}), func() {
		_, err := ReadVideo(t.Context(), platform.ReadInput{URL: "https://www.kuaishou.com/short-video/3xf6abc"})
		var re *platform.ReadError
		if !errors.As(err, &re) {
			t.Fatalf("expected ReadError, got %v", err)
		}
		if re.Code != platform.ErrCaptchaRequired {
			t.Fatalf("expected CAPTCHA_REQUIRED, got %+v", re)
		}
	})
}

func TestListUserVideosViaAPIMapsProfilePhotoList(t *testing.T) {
	withKuaishouAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"data": {
				"visionProfilePhotoList": {
					"result": 1,
					"feeds": [{
						"author": {"id": "u1", "name": "快手作者"},
						"photo": {"id": "3xf6abc", "caption": "列表视频", "timestamp": 1785231600}
					}]
				}
			}
		}`))
	}), func() {
		items, attempt, _, err := ListUserVideosViaAPI(t.Context(), &platform.NormalizedURL{
			PlatformID:   "u1",
			ContentKind:  "user",
			CanonicalURL: "https://www.kuaishou.com/profile/u1",
		}, platform.ReadInput{URL: "https://www.kuaishou.com/profile/u1"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if attempt == nil || !attempt.OK {
			t.Fatalf("expected ok attempt, got %+v", attempt)
		}
		if len(items) != 1 || items[0].ID != "kuaishou:3xf6abc" || items[0].Title != "列表视频" {
			t.Fatalf("unexpected items: %+v", items)
		}
		if items[0].PublishedAt.IsZero() {
			t.Fatalf("expected published time")
		}
	})
}

func withKuaishouAPIServer(t *testing.T, handler http.Handler, fn func()) {
	t.Helper()
	oldBase := kuaishouGraphQLBaseURL
	oldClient := kuaishouHTTPClient
	srv := httptest.NewServer(handler)
	defer srv.Close()
	kuaishouGraphQLBaseURL = srv.URL
	kuaishouHTTPClient = srv.Client()
	defer func() {
		kuaishouGraphQLBaseURL = oldBase
		kuaishouHTTPClient = oldClient
	}()
	fn()
}
