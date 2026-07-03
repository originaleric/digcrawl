package wechat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestParseArticleHTMLExtractsContentFromWechatDOM(t *testing.T) {
	t.Parallel()
	raw := readFixture(t, "article_ok.html")

	parsed, err := parseArticleHTML(raw, "https://mp.weixin.qq.com/s/test")
	if err != nil {
		t.Fatalf("parseArticleHTML() error = %v", err)
	}
	if parsed.Title != "测试文章标题" {
		t.Fatalf("unexpected title %q", parsed.Title)
	}
	if parsed.AuthorName != "测试公众号" {
		t.Fatalf("unexpected author %q", parsed.AuthorName)
	}
	if parsed.PublishedAt == "" {
		t.Fatalf("expected published_at from fixture")
	}
	if !strings.Contains(parsed.Text, "微信公众号正文") {
		t.Fatalf("expected body text, got %q", parsed.Text)
	}
	if !strings.Contains(parsed.Markdown, "微信公众号正文") {
		t.Fatalf("expected markdown body, got %q", parsed.Markdown)
	}
	if !strings.Contains(parsed.HTML, `src="https://mmbiz.qpic.cn/test/image-a.jpg"`) {
		t.Fatalf("expected data-src to be copied to src in HTML, got %s", parsed.HTML)
	}
	if len(parsed.ImageURLs) != 1 || parsed.ImageURLs[0] != "https://mmbiz.qpic.cn/test/image-a.jpg" {
		t.Fatalf("unexpected image urls: %#v", parsed.ImageURLs)
	}
}

func TestParseArticleHTMLUsesScriptFallbacks(t *testing.T) {
	t.Parallel()
	raw := readFixture(t, "article_script_fallback.html")

	parsed, err := parseArticleHTML(raw, "https://mp.weixin.qq.com/s/script")
	if err != nil {
		t.Fatalf("parseArticleHTML() error = %v", err)
	}
	if parsed.Title != "脚本标题" || parsed.TitleSource != "script.msg_title" {
		t.Fatalf("unexpected title fallback: %q source=%q", parsed.Title, parsed.TitleSource)
	}
	if parsed.AuthorName != "脚本作者" || parsed.AuthorSource != "script.nickname" {
		t.Fatalf("unexpected author fallback: %q source=%q", parsed.AuthorName, parsed.AuthorSource)
	}
}

func TestReadArticleHTTPReturnsContent(t *testing.T) {
	raw := readFixture(t, "article_ok.html")
	restore := stubHTTPClient(t, http.StatusOK, "https://mp.weixin.qq.com/s/test", raw, nil)
	defer restore()

	content, meta, err := readArticleHTTP(context.Background(), platform.ReadInput{
		URL:          "https://mp.weixin.qq.com/s/test",
		IncludeMedia: true,
	}, "https://mp.weixin.qq.com/s/test")
	if err != nil {
		t.Fatalf("readArticleHTTP() error = %v meta=%#v", err, meta)
	}
	if content.Title != "测试文章标题" {
		t.Fatalf("unexpected title %q", content.Title)
	}
	if content.HTML == "" || content.Markdown == "" || content.Text == "" {
		t.Fatalf("expected html/markdown/text from same DOM, got %+v", content)
	}
	if len(content.Media) != 1 {
		t.Fatalf("expected media from DOM, got %#v", content.Media)
	}
	if platform.MetaString(content.PlatformMetadata, "read_channel") != "http" {
		t.Fatalf("expected read_channel=http, got %#v", content.PlatformMetadata)
	}
	if platform.MetaString(content.PlatformMetadata, "http_parse_state") != wechatHTTPParseOK {
		t.Fatalf("expected http_parse_state=ok, got %#v", content.PlatformMetadata)
	}
}

func TestReadArticleHTTPClassifiesCaptchaRedirect(t *testing.T) {
	restore := stubHTTPClient(t, http.StatusOK, "https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha?poc_token=test", readFixture(t, "captcha_required.html"), []string{"https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha?poc_token=test"})
	defer restore()

	_, _, err := readArticleHTTP(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/article"}, "https://mp.weixin.qq.com/s/article")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrCaptchaRequired {
		t.Fatalf("expected %s, got %s", platform.ErrCaptchaRequired, re.Code)
	}
}

func TestReadArticleHTTPClassifiesKnownErrorPages(t *testing.T) {
	cases := []struct {
		name string
		file string
		code string
	}{
		{name: "rate limited", file: "rate_limited.html", code: platform.ErrRateLimited},
		{name: "deleted", file: "content_deleted.html", code: platform.ErrContentDeleted},
		{name: "private", file: "content_private.html", code: platform.ErrContentPrivate},
		{name: "not found", file: "content_not_found.html", code: platform.ErrContentNotFound},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := readFixture(t, tc.file)
			restore := stubHTTPClient(t, http.StatusOK, "https://mp.weixin.qq.com/s/"+tc.name, raw, nil)
			defer restore()

			_, _, err := readArticleHTTP(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/" + tc.name}, "https://mp.weixin.qq.com/s/"+tc.name)
			var re *platform.ReadError
			if !errors.As(err, &re) {
				t.Fatalf("expected ReadError, got %v", err)
			}
			if re.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, re.Code)
			}
		})
	}
}

func TestHTTPClassificationIgnoresScriptOnlyHints(t *testing.T) {
	t.Parallel()
	raw := readFixture(t, "article_ok.html") + `<script>window.weui.toast("请在手机微信中打开")</script>`

	code, msg, state := classifyWechatHTTPPage(raw, "https://mp.weixin.qq.com/s/test", nil)
	if code != "" || msg != "" || state != "" {
		t.Fatalf("expected no page-state error from script-only hint, got code=%q msg=%q state=%q", code, msg, state)
	}
}

func TestReadArticleHTTPPrefersValidArticleOverVisibleHintWords(t *testing.T) {
	raw := strings.Replace(readFixture(t, "article_ok.html"), "这里继续补充一些测试文本", "这里继续补充一些关于验证码和安全验证的普通文章文本", 1)
	restore := stubHTTPClient(t, http.StatusOK, "https://mp.weixin.qq.com/s/hints", raw, nil)
	defer restore()

	content, _, err := readArticleHTTP(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/hints"}, "https://mp.weixin.qq.com/s/hints")
	if err != nil {
		t.Fatalf("readArticleHTTP() should prefer valid article DOM over hint words, got %v", err)
	}
	if !strings.Contains(content.Text, "验证码") {
		t.Fatalf("expected article text with hint word, got %q", content.Text)
	}
}

func TestReadArticleHTTPFallsBackOnTransientHTTPFailure(t *testing.T) {
	restore := stubHTTPClient(t, http.StatusBadGateway, "https://mp.weixin.qq.com/s/fail", "temporary failure", nil)
	defer restore()

	_, meta, err := readArticleHTTP(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/fail"}, "https://mp.weixin.qq.com/s/fail")
	var fallback *httpFallbackError
	if !errors.As(err, &fallback) {
		t.Fatalf("expected httpFallbackError, got %T %v", err, err)
	}
	if platform.MetaString(meta, "http_error") == "" {
		t.Fatalf("expected http_error in metadata, got %#v", meta)
	}
}

func TestReadArticleHTTPRejectsOffDomainRedirect(t *testing.T) {
	raw := readFixture(t, "article_ok.html")
	restore := stubHTTPClient(t, http.StatusOK, "https://evil.example/landing", raw, []string{"https://evil.example/landing"})
	defer restore()

	_, meta, err := readArticleHTTP(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/redirect"}, "https://mp.weixin.qq.com/s/redirect")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %T %v", err, err)
	}
	if re.Code != platform.ErrScopeViolation {
		t.Fatalf("expected %s, got %s", platform.ErrScopeViolation, re.Code)
	}
	if !strings.Contains(platform.MetaString(meta, "http_error"), "不在允许域名列表") {
		t.Fatalf("expected domain rejection metadata, got %#v", meta)
	}
}

func TestReadArticleHTTPRejectsOffDomainRedirectBeforeResponse(t *testing.T) {
	old := newWechatHTTPClient
	newWechatHTTPClient = func(redirectsOut *[]string) *http.Client {
		*redirectsOut = append(*redirectsOut, "https://evil.example/landing")
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, &url.Error{
					Op:  "Get",
					URL: "https://evil.example/landing",
					Err: newWechatScopeError("https://evil.example/landing", "微信 HTTP 跳转目标 evil.example 不在允许域名列表"),
				}
			}),
		}
	}
	defer func() { newWechatHTTPClient = old }()

	_, meta, err := readArticleHTTP(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/redirect"}, "https://mp.weixin.qq.com/s/redirect")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %T %v", err, err)
	}
	if re.Code != platform.ErrScopeViolation {
		t.Fatalf("expected %s, got %s", platform.ErrScopeViolation, re.Code)
	}
	if !strings.Contains(platform.MetaString(meta, "http_error"), "不在允许域名列表") {
		t.Fatalf("expected redirect rejection metadata, got %#v", meta)
	}
}

func TestReadArticleRejectsOffDomainHTTPWithoutBrowserFallback(t *testing.T) {
	raw := readFixture(t, "article_ok.html")
	restoreHTTP := stubHTTPClient(t, http.StatusOK, "https://evil.example/landing", raw, []string{"https://evil.example/landing"})
	defer restoreHTTP()

	oldBrowse := articleBrowseFn
	articleBrowseFn = func(ctx context.Context, url string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
		t.Fatalf("browser fallback should not be called after HTTP scope violation")
		return nil, "", nil
	}
	defer func() { articleBrowseFn = oldBrowse }()

	_, err := ReadArticle(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/redirect"})
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %T %v", err, err)
	}
	if re.Code != platform.ErrScopeViolation {
		t.Fatalf("expected %s, got %s", platform.ErrScopeViolation, re.Code)
	}
}

func TestReadArticleUsesHTTPBeforeBrowser(t *testing.T) {
	raw := readFixture(t, "article_ok.html")
	restoreHTTP := stubHTTPClient(t, http.StatusOK, "https://mp.weixin.qq.com/s/test", raw, nil)
	defer restoreHTTP()

	oldBrowse := articleBrowseFn
	articleBrowseFn = func(ctx context.Context, url string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
		t.Fatalf("browser fallback should not be called for HTTP-readable article")
		return nil, "", nil
	}
	defer func() { articleBrowseFn = oldBrowse }()

	content, err := ReadArticle(context.Background(), platform.ReadInput{URL: "https://mp.weixin.qq.com/s/test", IncludeMedia: true})
	if err != nil {
		t.Fatalf("ReadArticle() error = %v", err)
	}
	if content.Title != "测试文章标题" {
		t.Fatalf("unexpected title %q", content.Title)
	}
	if platform.MetaString(content.PlatformMetadata, "read_channel") != "http" {
		t.Fatalf("expected read_channel=http, got %#v", content.PlatformMetadata)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubHTTPClient(t *testing.T, status int, finalURL string, body string, redirects []string) func() {
	t.Helper()
	old := newWechatHTTPClient
	newWechatHTTPClient = func(redirectsOut *[]string) *http.Client {
		if len(redirects) > 0 {
			*redirectsOut = append(*redirectsOut, redirects...)
		}
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				u, err := url.Parse(finalURL)
				if err != nil {
					t.Fatalf("parse finalURL: %v", err)
				}
				finalReq := req.Clone(req.Context())
				finalReq.URL = u
				return &http.Response{
					StatusCode: status,
					Status:     http.StatusText(status),
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    finalReq,
				}, nil
			}),
		}
	}
	return func() { newWechatHTTPClient = old }
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/fixtures/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}
