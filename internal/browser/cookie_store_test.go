package browser

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

type cookieSetPage struct {
	cookies []*proto.NetworkCookieParam
}

func (p *cookieSetPage) SetCookies(cookies []*proto.NetworkCookieParam) error {
	p.cookies = append([]*proto.NetworkCookieParam(nil), cookies...)
	return nil
}

type cookieReadPage struct {
	cookies []*proto.NetworkCookie
}

func (p *cookieReadPage) Cookies(urls []string) ([]*proto.NetworkCookie, error) {
	return p.cookies, nil
}

func TestLoadDomainCookiesQuarantinesCorruptPrimaryAndFallsBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primary := filepath.Join(dir, "www.toutiao.com.json")
	if err := os.WriteFile(primary, []byte(`[{"name":"bad"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback := []*proto.NetworkCookieParam{{
		Name:   "sessionid",
		Value:  "ok",
		Domain: ".toutiao.com",
		Path:   "/",
	}}
	data, err := json.Marshal(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "toutiao.com.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	page := &cookieSetPage{}
	targetURL := mustParseURL(t, "https://www.toutiao.com/video/1/")
	if err := loadDomainCookies(page, "www.toutiao.com", dir, targetURL); err != nil {
		t.Fatal(err)
	}
	if len(page.cookies) != 1 || page.cookies[0].Name != "sessionid" || page.cookies[0].Value != "ok" {
		t.Fatalf("fallback cookies not loaded: %+v", page.cookies)
	}
	if _, err := os.Stat(primary); !os.IsNotExist(err) {
		t.Fatalf("corrupt primary should be quarantined, stat err=%v", err)
	}
	matches, err := filepath.Glob(primary + ".bad.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one quarantined cookie file, got %d", len(matches))
	}
}

func TestSaveDomainCookiesWritesValidJSONAtomically(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	page := &cookieReadPage{cookies: []*proto.NetworkCookie{{
		Name:   "sessionid",
		Value:  "fresh",
		Domain: ".toutiao.com",
		Path:   "/",
	}}}
	targetURL := mustParseURL(t, "https://www.toutiao.com/video/1/")
	if err := saveDomainCookies(page, "www.toutiao.com", dir, targetURL); err != nil {
		t.Fatal(err)
	}

	cookiePath := filepath.Join(dir, "www.toutiao.com.json")
	data, err := os.ReadFile(cookiePath)
	if err != nil {
		t.Fatal(err)
	}
	var cookies []*proto.NetworkCookieParam
	if err := json.Unmarshal(data, &cookies); err != nil {
		t.Fatalf("saved cookie file is not valid JSON: %v", err)
	}
	if len(cookies) != 1 || cookies[0].Name != "sessionid" || cookies[0].Value != "fresh" {
		t.Fatalf("unexpected saved cookies: %+v", cookies)
	}
	if info, err := os.Stat(cookiePath); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		got := info.Mode().Perm()
		t.Fatalf("cookie file perm=%o, want 600", got)
	}
	tmpMatches, err := filepath.Glob(filepath.Join(dir, ".www.toutiao.com.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpMatches) != 0 {
		t.Fatalf("temporary files should be cleaned up: %v", tmpMatches)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
