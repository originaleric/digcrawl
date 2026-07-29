package weibo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchPostViaAPIClassifiesOKZeroAsFallback(t *testing.T) {
	oldBaseURL := weiboAPIBaseURL
	oldClient := weiboHTTPClient
	defer func() {
		weiboAPIBaseURL = oldBaseURL
		weiboHTTPClient = oldClient
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":0,"msg":"登录后查看更多","id":"502233445566","text_raw":"stale payload"}`))
	}))
	defer srv.Close()
	weiboAPIBaseURL = srv.URL
	weiboHTTPClient = srv.Client()

	_, attempt, _, err := FetchPostViaAPI(t.Context(), &platform.NormalizedURL{
		PlatformID:   "502233445566",
		ContentKind:  "post",
		CanonicalURL: "https://m.weibo.cn/detail/502233445566",
	})
	if err == nil {
		t.Fatal("expected ok=0 response to fail")
	}
	if attempt == nil || attempt.OK || !attempt.FallbackBrowser {
		t.Fatalf("expected browser fallback attempt, got %+v", attempt)
	}
	if attempt.ErrorCode != "api_success_false" {
		t.Fatalf("expected api_success_false, got %+v", attempt)
	}
}
