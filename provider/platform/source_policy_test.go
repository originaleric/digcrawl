package platformprov

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/bilibili"
	"github.com/originaleric/digcrawl/internal/platform/reddit"
	"github.com/originaleric/digcrawl/internal/platform/wechat"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/registry"
)

// Exercises the real WeChat HTTP client, redirect checks, parser and provider.
// Only the HTTP dependency is simulated; no remote account or cookies are used.
type sourcePolicyWechatTransport struct{ body []byte }

func (tr sourcePolicyWechatTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req}
	switch req.URL.Hostname() {
	case "mp.weixin.qq.com":
		resp.StatusCode = http.StatusFound
		resp.Header.Set("Location", "https://www.weixin.qq.com/s/final")
		resp.Body = io.NopCloser(strings.NewReader(""))
	case "www.weixin.qq.com":
		resp.Body = io.NopCloser(bytes.NewReader(tr.body))
	default:
		return nil, fmt.Errorf("unexpected fixture host: %s", req.URL.Hostname())
	}
	return resp, nil
}

func TestChromePlatformSourcePolicyAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	previous := *config.Get()
	// Route the real browser's Bilibili page to an isolated TLS fixture.
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Split(r.Host, ":")[0] == "www.bilibili.com" && r.URL.Path != "/clip.mp4" {
			http.Redirect(w, r, "https://outside.example/video", http.StatusFound)
			return
		}
		if r.URL.Path == "/clip.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			fmt.Fprint(w, "fixture media")
			return
		}
		fmt.Fprint(w, `<html><head><title>Browser title</title></head><body><article>Browser text</article><video style="display:none" preload="none" src="/clip.mp4"></video></body></html>`)
	}))
	defer tlsServer.Close()
	fixtureAddr := strings.TrimPrefix(tlsServer.URL, "https://")
	headless := true
	config.Get().LocalBrowser = config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 20, NavigateTimeoutSec: 5, WaitSelectorTimeoutSec: 1}
	config.Get().Gateway.AllowedDomains = nil
	config.Get().LocalBrowser.CookieStoreDir = t.TempDir()
	config.Get().LocalBrowser.ExtraArgs = []string{"--host-resolver-rules=MAP www.bilibili.com " + fixtureAddr + ", MAP outside.example " + fixtureAddr, "--ignore-certificate-errors", "--no-proxy-server"}
	defer func() {
		if err := browser.ShutdownImmediately(context.Background()); err != nil {
			t.Error(err)
		}
		*config.Get() = previous
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://"+strings.Replace(r.Host, "localhost", "127.0.0.1", 1)+"/final", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<html><head><title>Platform body</title></head><body><article>confirmed platform body</article></body></html>`)
	}))
	defer server.Close()
	inputURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/redirect"
	input, _ := json.Marshal(platformReadInput{URL: inputURL})
	var observedFinalURL string
	handler := platformReadHandlerNamedWithConfig(ToolToutiaoArticleRead, nil, nil, nil, func(ctx context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
		resp, err := iplatform.Browse(ctx, in.URL, in, iplatform.BrowseSpec{ContentSelector: "article", MetadataScript: `() => ({final_url:'https://declared.invalid/',text:'confirmed platform body'})`})
		if err != nil {
			return nil, err
		}
		observedFinalURL = iplatform.MetaString(resp.Metadata, "final_url")
		return &iplatform.Content{SourceURL: in.URL, Text: resp.Text, PlatformMetadata: resp.Metadata}, nil
	}, &config.Config{})
	for _, allowRedirect := range []bool{false, true} {
		domains := []string{"localhost"}
		if allowRedirect {
			domains = append(domains, "127.0.0.1")
		}
		out, arts, err := handler(context.Background(), &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: domains}})
		if observedFinalURL != server.URL+"/final" {
			t.Fatalf("browser-confirmed URL lost: %q", observedFinalURL)
		}
		if !allowRedirect {
			if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil || len(arts) != 0 {
				t.Fatalf("out=%v arts=%v err=%v", out, arts, err)
			}
		} else if err != nil || out["text"] != "confirmed platform body" {
			t.Fatalf("out=%v err=%v", out, err)
		}
	}
	t.Run("bilibili_merged_source", func(t *testing.T) {
		oldTransport := http.DefaultClient.Transport
		http.DefaultClient.Transport = sourcePolicyBilibiliTransport{fallback: oldTransport}
		defer func() { http.DefaultClient.Transport = oldTransport }()
		input, _ := json.Marshal(platformReadInput{URL: "https://www.bilibili.com/video/BV1xx411c7mD", IncludeMedia: true})
		handler := platformReadHandlerNamedWithConfig(ToolBilibiliVideoRead, bilibiliDefaultDomains, nil, nil, bilibili.ReadVideo, &config.Config{})
		for _, allow := range []bool{false, true} {
			domains := []string{"www.bilibili.com"}
			if allow {
				domains = append(domains, "outside.example")
			}
			out, arts, err := handler(context.Background(), &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: domains}})
			if !allow {
				if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil || len(arts) != 0 {
					t.Fatalf("out=%v arts=%v err=%v", out, arts, err)
				}
				continue
			}
			if err != nil || out["title"] != "API title" || !strings.Contains(fmt.Sprint(out["text"]), "API body") || out["platform_metadata"].(map[string]any)["final_url"] != "https://outside.example/video" || len(out["media"].([]iplatform.MediaItem)) == 0 {
				t.Fatalf("out=%v err=%v", out, err)
			}
		}
	})

}

func TestWechatRedirectRespectsFinalSourcePolicy(t *testing.T) {
	body, err := os.ReadFile("../../internal/platform/wechat/testdata/fixtures/article_ok.html")
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = sourcePolicyWechatTransport{body: body}
	defer func() { http.DefaultTransport = previous }()
	input, _ := json.Marshal(platformReadInput{URL: "https://mp.weixin.qq.com/s/start"})
	for _, tc := range []struct {
		name          string
		call, config  []string
		wantRejection bool
	}{
		{name: "call_restriction", call: []string{"mp.weixin.qq.com"}, wantRejection: true},
		{name: "config_restriction", config: []string{"mp.weixin.qq.com"}, wantRejection: true},
		{name: "platform_default_allows_redirect"},
		{name: "call_overrides_config", call: []string{"weixin.qq.com"}, config: []string{"mp.weixin.qq.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := platformReadHandlerNamedWithConfig(ToolWechatArticleRead, wechatDefaultDomains, tc.config, nil, wechat.ReadArticle, &config.Config{})
			out, arts, err := handler(context.Background(), &protocol.ToolCall{Tool: ToolWechatArticleRead, Input: input, Policy: protocol.CallPolicy{AllowedDomains: tc.call}})
			if tc.wantRejection {
				if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil || len(arts) != 0 {
					t.Fatalf("out=%v arts=%v err=%v", out, arts, err)
				}
			} else if err != nil || out["source_url"] != "https://www.weixin.qq.com/s/final" || out["text"] == "" {
				t.Fatalf("out=%v err=%v", out, err)
			}
		})
	}
}

func TestPlatformSourceRejectionPrecedesEnhancementAndStorage(t *testing.T) {
	for _, redditEntry := range []bool{false, true} {
		for _, collect := range []bool{false, true} {
			for _, field := range []string{"source_url", "final_url", "resolved_url", "empty_source", "nil_content"} {
				t.Run(fmt.Sprintf("reddit=%v/collect=%v/%s", redditEntry, collect, field), func(t *testing.T) {
					var downloads atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						downloads.Add(1)
						w.Header().Set("Content-Type", "image/png")
						fmt.Fprint(w, "should not be downloaded")
					}))
					defer server.Close()
					retained := filepath.Join(t.TempDir(), "images")
					enabled := true
					cfg := &config.Config{
						Skill:         config.SkillConfig{OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub"}},
						ImageDownload: config.ImageDownloadConfig{Dir: retained, Providers: map[string]bool{"reddit": true, "toutiao": true}},
					}
					store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
					if err != nil {
						t.Fatal(err)
					}
					content := &iplatform.Content{
						SourceURL: "https://allowed.example/article", Markdown: "body", Text: "body",
						ScreenshotBase64: "iVBORw0KGgo=", Media: []iplatform.MediaItem{{Type: "image", URL: server.URL + "/a.png"}},
						PlatformMetadata: map[string]any{},
					}
					switch field {
					case "source_url":
						content.SourceURL = "https://blocked.example/article"
					case "empty_source":
						content.SourceURL = ""
					case "nil_content":
						content = nil
					default:
						content.PlatformMetadata[field] = "https://blocked.example/article"
					}
					readerCalls := 0
					handler := platformReadHandlerNamedWithConfig(ToolToutiaoArticleRead, nil, nil, store, func(context.Context, iplatform.ReadInput) (*iplatform.Content, error) {
						readerCalls++
						return content, nil
					}, cfg)
					if redditEntry {
						handler = redditPostReadHandler(nil, store, func(context.Context, reddit.ReadInput) (*iplatform.Content, error) {
							readerCalls++
							return content, nil
						}, cfg)
					}
					input, _ := json.Marshal(platformReadInput{URL: "https://allowed.example/article", IncludeOCR: true, IncludeScreenshot: true})
					ctx := context.Background()
					if collect {
						ctx = executor.WithCollectExecution(ctx)
					}
					out, arts, err := handler(ctx, &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: []string{"allowed.example"}}})
					code := "DOMAIN_NOT_ALLOWED"
					if field == "empty_source" || field == "nil_content" {
						code = "INVALID_INPUT"
					}
					if err == nil || !strings.Contains(err.Error(), code) || out != nil || len(arts) != 0 || readerCalls != 1 || downloads.Load() != 0 {
						t.Fatalf("out=%v arts=%v err=%v readerCalls=%d downloads=%d", out, arts, err, readerCalls, downloads.Load())
					}
					if _, err := os.Stat(retained); !os.IsNotExist(err) {
						t.Fatalf("rejected source caused local image storage: %v", err)
					}
					files, err := os.ReadDir(store.BaseDir)
					if err != nil || len(files) != 0 {
						t.Fatalf("rejected source persisted artifact: files=%v err=%v", files, err)
					}
				})
			}
		}
	}
}

func TestFeedRedirectRespectsFinalSourcePolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://"+strings.Replace(r.Host, "localhost", "127.0.0.1", 1)+"/feed.xml", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><title>Feed</title><item><title>Item</title><link>https://item.example/article</link></item></channel></rss>`)
	}))
	defer server.Close()
	inputURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/redirect"
	input, _ := json.Marshal(platformReadInput{URL: inputURL})
	handler := FeedReadEntry(nil, nil).Handler
	for _, allowRedirect := range []bool{false, true} {
		domains := []string{"localhost"}
		if allowRedirect {
			domains = append(domains, "127.0.0.1")
		}
		out, _, err := handler(context.Background(), &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: domains}})
		if !allowRedirect {
			if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil {
				t.Fatalf("out=%v err=%v", out, err)
			}
		} else if err != nil || out["source_url"] != inputURL || out["platform_metadata"].(map[string]any)["final_url"] != server.URL+"/feed.xml" {
			t.Fatalf("out=%v err=%v", out, err)
		}
	}
}

type sourcePolicyRedditRSSTransport struct{}

func (sourcePolicyRedditRSSTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Request: req}
	switch req.URL.Hostname() {
	case "www.reddit.com":
		if !strings.HasSuffix(req.URL.Path, "/.rss") {
			return nil, fmt.Errorf("unexpected fixture path: %s", req.URL.Path)
		}
		resp.StatusCode = http.StatusFound
		resp.Header.Set("Location", "https://outside.example/feed.xml")
		resp.Body = io.NopCloser(strings.NewReader(""))
	case "outside.example":
		resp.Body = io.NopCloser(strings.NewReader(`<rss version="2.0"><channel><title>Remote feed</title><item><title>Outside source</title><link>https://www.reddit.com/r/golang/comments/abc123/post/</link><description>Body from outside domain</description></item></channel></rss>`))
	default:
		return nil, fmt.Errorf("unexpected fixture host: %s", req.URL.Hostname())
	}
	return resp, nil
}

// Covers every RSS consumer through the real Reddit reader/provider boundary.
func TestRedditRSSSourcePolicyAcrossEntries(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = sourcePolicyRedditRSSTransport{}
	defer func() { http.DefaultTransport = previousTransport }()
	previous := *config.Get()
	disabled := false
	config.Get().Platform.Reddit.APIEnabled = &disabled
	defer func() { *config.Get() = previous }()
	for _, entry := range []registry.Entry{RedditPostReadEntry(nil, nil, &config.Config{}), RedditSubredditReadEntry(nil), RedditUserReadEntry(nil)} {
		for _, tc := range []struct {
			name         string
			call, config []string
			allow        bool
		}{
			{name: "default"},
			{name: "call_restriction", call: []string{"www.reddit.com"}},
			{name: "config_restriction", config: []string{"www.reddit.com"}},
			{name: "call_overrides_config", call: []string{"www.reddit.com", "outside.example"}, config: []string{"www.reddit.com"}, allow: true},
		} {
			t.Run(entry.Descriptor.Name+"/"+tc.name, func(t *testing.T) {
				handler := entry.Handler
				switch entry.Descriptor.Name {
				case ToolRedditPostRead:
					handler = RedditPostReadEntry(tc.config, nil, &config.Config{}).Handler
				case ToolRedditSubredditRead:
					handler = RedditSubredditReadEntry(tc.config).Handler
				case ToolRedditUserRead:
					handler = RedditUserReadEntry(tc.config).Handler
				}
				input, _ := json.Marshal(map[string]any{"url": "https://www.reddit.com/r/golang/comments/abc123/post/", "subreddit": "golang", "username": "fixture"})
				out, arts, err := handler(context.Background(), &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: tc.call}})
				if !tc.allow {
					if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil || len(arts) != 0 {
						t.Fatalf("out=%v arts=%v err=%v", out, arts, err)
					}
					return
				}
				if err != nil || out == nil {
					t.Fatalf("out=%v err=%v", out, err)
				}
				if entry.Descriptor.Name == ToolRedditPostRead && out["platform_metadata"].(map[string]any)["final_url"] != "https://outside.example/feed.xml" {
					t.Fatalf("source lost: %v", out)
				}
			})
		}
	}
}

// Only the API dependency is simulated; Chromium follows a real TLS redirect.
type sourcePolicyBilibiliTransport struct{ fallback http.RoundTripper }

func (tr sourcePolicyBilibiliTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() != "api.bilibili.com" {
		return tr.fallback.RoundTrip(req)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"bvid":"BV1xx411c7mD","title":"API title","desc":"API body"}}`))}, nil
}

func TestPlatformErrorMetadataRespectsSourcePolicy(t *testing.T) {
	for _, redditEntry := range []bool{false, true} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("reddit=%v/allow=%v", redditEntry, allow), func(t *testing.T) {
				re := iplatform.NewReadError(iplatform.ErrPartialContent, "fixture partial result", "fixture", map[string]any{"final_url": "https://outside.example/article", "text": "partial page body"})
				handler := platformReadHandlerNamedWithConfig(ToolToutiaoArticleRead, nil, nil, nil, func(context.Context, iplatform.ReadInput) (*iplatform.Content, error) { return nil, re }, &config.Config{})
				if redditEntry {
					handler = redditPostReadHandler(nil, nil, func(context.Context, reddit.ReadInput) (*iplatform.Content, error) { return nil, re }, &config.Config{})
				}
				input, _ := json.Marshal(platformReadInput{URL: "https://allowed.example/article"})
				domains := []string{"allowed.example"}
				if allow {
					domains = append(domains, "outside.example")
				}
				out, arts, err := handler(context.Background(), &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: domains}})
				if !allow {
					if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil || len(arts) != 0 {
						t.Fatalf("out=%v arts=%v err=%v", out, arts, err)
					}
					return
				}
				if err != re || out["platform_metadata"].(map[string]any)["error_code"] != iplatform.ErrPartialContent {
					t.Fatalf("allowed partial result changed: out=%v err=%v", out, err)
				}
			})
		}
	}
}
