package browser

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestSaveDomainCookiesWritesCanonicalSessionAtomically(t *testing.T) {
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

	cookiePath := filepath.Join(dir, "toutiao.com.session.json")
	data, err := os.ReadFile(cookiePath)
	if err != nil {
		t.Fatal(err)
	}
	var session cookieSession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("saved cookie session is not valid JSON: %v", err)
	}
	if session.Version != cookieSessionSchemaVersion || session.Domain != "toutiao.com" || session.UpdatedAt.IsZero() {
		t.Fatalf("unexpected session metadata: %+v", session)
	}
	if len(session.Cookies) != 1 || session.Cookies[0].Name != "sessionid" || session.Cookies[0].Value != "fresh" {
		t.Fatalf("unexpected saved cookies: %+v", session.Cookies)
	}
	if info, err := os.Stat(cookiePath); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		got := info.Mode().Perm()
		t.Fatalf("cookie file perm=%o, want 600", got)
	}
	tmpMatches, err := filepath.Glob(filepath.Join(dir, ".toutiao.com.session.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpMatches) != 0 {
		t.Fatalf("temporary files should be cleaned up: %v", tmpMatches)
	}
}

func TestCanonicalSessionIsSharedByRootAndWWW(t *testing.T) {
	dir := t.TempDir()
	page := &cookieReadPage{cookies: []*proto.NetworkCookie{{
		Name:   "sessionid",
		Value:  "fresh",
		Domain: ".toutiao.com",
		Path:   "/",
	}}}
	if err := saveDomainCookies(page, "www.toutiao.com", dir, mustParseURL(t, "https://www.toutiao.com/")); err != nil {
		t.Fatal(err)
	}
	header, err := CookieHeaderForURL(dir, "https://toutiao.com/api/pc/user/info")
	if err != nil {
		t.Fatal(err)
	}
	if header != "sessionid=fresh" {
		t.Fatalf("root domain must reuse www session, header=%q", header)
	}
}

func TestCanonicalCookieDomainSharesOnlyRootAndWWW(t *testing.T) {
	cases := map[string]string{
		"www.toutiao.com":  "toutiao.com",
		"toutiao.com":      "toutiao.com",
		"mp.weixin.qq.com": "mp.weixin.qq.com",
	}
	for domain, want := range cases {
		got, err := canonicalCookieDomain(domain)
		if err != nil || got != want {
			t.Errorf("canonicalCookieDomain(%q)=(%q, %v), want %q", domain, got, err, want)
		}
	}
}

func TestCookieHeaderForURLUsesOnlyValidMatchingCookies(t *testing.T) {
	dir := t.TempDir()
	cookies := []*proto.NetworkCookieParam{
		{Name: "session", Value: "active", Domain: ".toutiao.com", Path: "/", Secure: true},
		{Name: "expired", Value: "old", Domain: ".toutiao.com", Path: "/", Expires: 1},
		{Name: "other", Value: "no", Domain: ".example.com", Path: "/"},
	}
	data, err := json.Marshal(cookies)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "www.toutiao.com.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	header, err := CookieHeaderForURL(dir, "https://www.toutiao.com/c/user/token/abc")
	if err != nil {
		t.Fatal(err)
	}
	if header != "session=active" {
		t.Fatalf("cookie header=%q", header)
	}
}

func TestCookieStoreReadsCompatibleLegacyDomainsBeforeExplicitLogin(t *testing.T) {
	dir := t.TempDir()
	writeCookieSnapshot(t, dir, "www.toutiao.com", []*proto.NetworkCookieParam{
		{Name: "host", Value: "primary", Domain: "www.toutiao.com", Path: "/"},
		{Name: "shared", Value: "primary", Domain: ".toutiao.com", Path: "/"},
	})
	writeCookieSnapshot(t, dir, "toutiao.com", []*proto.NetworkCookieParam{
		{Name: "root", Value: "active", Domain: ".toutiao.com", Path: "/"},
		{Name: "shared", Value: "stale", Domain: ".toutiao.com", Path: "/"},
	})

	targetURL := mustParseURL(t, "https://www.toutiao.com/api/pc/user/info?token=abc")
	page := &cookieSetPage{}
	if err := loadDomainCookies(page, "www.toutiao.com", dir, targetURL); err != nil {
		t.Fatal(err)
	}
	if got := cookieValues(page.cookies); got != "host=primary; shared=primary; root=active" {
		t.Fatalf("browser cookies=%q", got)
	}

	header, err := CookieHeaderForURL(dir, targetURL.String())
	if err != nil {
		t.Fatal(err)
	}
	if header != "host=primary; shared=primary; root=active" {
		t.Fatalf("cookie header=%q", header)
	}
	if _, err := os.Stat(filepath.Join(dir, "toutiao.com.session.json")); !os.IsNotExist(err) {
		t.Fatalf("background compatibility read must not create a session, err=%v", err)
	}
}

func TestCookieStoreIgnoresLegacyFilesAfterMigration(t *testing.T) {
	dir := t.TempDir()
	writeCookieSnapshot(t, dir, "www.toutiao.com", []*proto.NetworkCookieParam{{Name: "session", Value: "first", Domain: ".toutiao.com", Path: "/"}})
	page := &cookieReadPage{cookies: []*proto.NetworkCookie{{Name: "session", Value: "logged-in", Domain: ".toutiao.com", Path: "/"}}}
	if err := saveDomainCookies(page, "www.toutiao.com", dir, mustParseURL(t, "https://www.toutiao.com/")); err != nil {
		t.Fatal(err)
	}
	writeCookieSnapshot(t, dir, "www.toutiao.com", []*proto.NetworkCookieParam{{Name: "session", Value: "legacy-late-write", Domain: ".toutiao.com", Path: "/"}})

	header, err := CookieHeaderForURL(dir, "https://www.toutiao.com/api/pc/user/info")
	if err != nil {
		t.Fatal(err)
	}
	if header != "session=logged-in" {
		t.Fatalf("canonical session must not merge later legacy writes, header=%q", header)
	}
}

func TestCookieStoreDoesNotFallbackToLegacyAfterSessionCorruption(t *testing.T) {
	dir := t.TempDir()
	writeCookieSnapshot(t, dir, "www.toutiao.com", []*proto.NetworkCookieParam{{Name: "session", Value: "legacy", Domain: ".toutiao.com", Path: "/"}})
	page := &cookieReadPage{cookies: []*proto.NetworkCookie{{Name: "session", Value: "fresh", Domain: ".toutiao.com", Path: "/"}}}
	if err := saveDomainCookies(page, "www.toutiao.com", dir, mustParseURL(t, "https://www.toutiao.com/")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "toutiao.com.session.json"), []byte(`{not-json`), 0o600); err != nil {
		t.Fatal(err)
	}

	header, err := CookieHeaderForURL(dir, "https://www.toutiao.com/api/pc/user/info")
	if err == nil || header != "" {
		t.Fatalf("corrupted session must require recovery instead of legacy fallback, header=%q err=%v", header, err)
	}
	header, err = CookieHeaderForURL(dir, "https://www.toutiao.com/api/pc/user/info")
	if err != nil || header != "" {
		t.Fatalf("retired legacy cookies must not revive after recovery, header=%q err=%v", header, err)
	}
}

func TestInteractiveLoginSaveRetiresLegacySnapshots(t *testing.T) {
	dir := t.TempDir()
	writeCookieSnapshot(t, dir, "www.toutiao.com", []*proto.NetworkCookieParam{{Name: "session", Value: "legacy", Domain: ".toutiao.com", Path: "/"}})
	target := mustParseURL(t, "https://www.toutiao.com/")
	page := &cookieReadPage{cookies: []*proto.NetworkCookie{{Name: "session", Value: "fresh-login", Domain: ".toutiao.com", Path: "/"}}}
	if err := saveDomainCookies(page, "www.toutiao.com", dir, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "www.toutiao.com.json")); !os.IsNotExist(err) {
		t.Fatalf("interactive login must retire legacy snapshot, err=%v", err)
	}
	header, err := CookieHeaderForURL(dir, target.String())
	if err != nil || header != "session=fresh-login" {
		t.Fatalf("fresh login header=%q err=%v", header, err)
	}
}

func TestCookiePathMatchesUsesDirectoryBoundaries(t *testing.T) {
	cases := []struct {
		requestPath string
		cookiePath  string
		want        bool
	}{
		{"/api", "/api", true},
		{"/api/v1", "/api", true},
		{"/apiary", "/api", false},
		{"/apiary", "/api/", false},
		{"/api/v1", "/api/", true},
		{"/anything", "/", true},
		{"/anything", "", true},
	}
	for _, tc := range cases {
		if got := cookiePathMatches(tc.requestPath, tc.cookiePath); got != tc.want {
			t.Errorf("cookiePathMatches(%q, %q)=%v want %v", tc.requestPath, tc.cookiePath, got, tc.want)
		}
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

func writeCookieSnapshot(t *testing.T, dir, domain string, cookies []*proto.NetworkCookieParam) {
	t.Helper()
	data, err := json.Marshal(cookies)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, domain+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func cookieValues(cookies []*proto.NetworkCookieParam) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie != nil {
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
	}
	return strings.Join(parts, "; ")
}
