package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

const cookieSessionSchemaVersion = 1

var cookieDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
var cookieFileLocks sync.Map

// cookieSession is the sole persisted login state for one logical web site.
// InteractiveLogin is its only production writer; Watchlist and browser tools
// only read it before navigating.
type cookieSession struct {
	Version   int                         `json:"version"`
	Domain    string                      `json:"domain"`
	UpdatedAt time.Time                   `json:"updated_at"`
	Cookies   []*proto.NetworkCookieParam `json:"cookies"`
}

// CookieSessionUnavailableError identifies a session that cannot safely be
// used and needs an explicit fresh browser login. It is distinct from ordinary
// file-system errors so Watchlist does not mislabel transient I/O as logout.
type CookieSessionUnavailableError struct{ Err error }

func (e *CookieSessionUnavailableError) Error() string {
	if e == nil || e.Err == nil {
		return "browser cookie session is unavailable"
	}
	return "browser cookie session is unavailable: " + e.Err.Error()
}

func (e *CookieSessionUnavailableError) Unwrap() error { return e.Err }

func newCookieSessionUnavailableError(err error) error {
	if err == nil {
		return nil
	}
	return &CookieSessionUnavailableError{Err: err}
}

func sanitizeCookieDomain(domain string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(d, ".")
	if d == "" {
		return "", fmt.Errorf("cookie 域名为空")
	}
	if !cookieDomainPattern.MatchString(d) {
		return "", fmt.Errorf("cookie 域名格式非法")
	}
	return d, nil
}

// canonicalCookieDomain only joins the conventional root/www host pair.
// Other subdomains stay isolated because registrable-domain guesses are unsafe
// for hosted domains such as mp.weixin.qq.com.
func canonicalCookieDomain(domain string) (string, error) {
	d, err := sanitizeCookieDomain(domain)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(d, "www."), nil
}

// cookieFilePath is retained solely to import the pre-session cookie files.
func cookieFilePath(cookieStoreDir, domain string) (string, error) {
	d, err := sanitizeCookieDomain(domain)
	if err != nil {
		return "", err
	}
	return filepath.Join(cookieStoreDir, d+".json"), nil
}

func cookieSessionFilePath(cookieStoreDir, domain string) (string, string, error) {
	canonical, err := canonicalCookieDomain(domain)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(cookieStoreDir, canonical+".session.json"), canonical, nil
}

func cookieFileMutex(cookiePath string) *sync.Mutex {
	key := filepath.Clean(cookiePath)
	mu, _ := cookieFileLocks.LoadOrStore(key, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func loadDomainCookies(page interface {
	SetCookies(cookies []*proto.NetworkCookieParam) error
}, domain, cookieStoreDir string, targetURL *url.URL) error {
	cookies, loaded, err := readCookieParamsForDomain(cookieStoreDir, domain)
	if err != nil || !loaded || len(cookies) == 0 {
		return err
	}
	for _, cookie := range cookies {
		if cookie != nil && cookie.URL == "" {
			cookie.URL = targetURL.Scheme + "://" + targetURL.Host
		}
	}
	if err := page.SetCookies(cookies); err != nil {
		return fmt.Errorf("注入 cookie 失败: %w", err)
	}
	return nil
}

// CookieHeaderForURL derives a same-origin HTTP Cookie header from the same
// canonical session used by browser navigation.
func CookieHeaderForURL(cookieStoreDir, rawURL string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme == "" || target.Hostname() == "" {
		return "", fmt.Errorf("cookie target URL is invalid")
	}
	cookies, loaded, err := readCookieParamsForDomain(cookieStoreDir, target.Hostname())
	if err != nil || !loaded {
		return "", err
	}

	host := strings.ToLower(target.Hostname())
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	now := float64(time.Now().Unix())
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		if cookie.Secure && !strings.EqualFold(target.Scheme, "https") {
			continue
		}
		if cookie.Expires > 0 && float64(cookie.Expires) <= now {
			continue
		}
		cookieDomain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cookie.Domain)), ".")
		if cookieDomain != "" && host != cookieDomain && !strings.HasSuffix(host, "."+cookieDomain) {
			continue
		}
		if !cookiePathMatches(path, cookie.Path) {
			continue
		}
		if strings.ContainsAny(cookie.Name, ";\r\n") || strings.ContainsAny(cookie.Value, ";\r\n") {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; "), nil
}

func cookiePathMatches(requestPath, cookiePath string) bool {
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		requestPath = "/"
	}
	cookiePath = strings.TrimSpace(cookiePath)
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || requestPath[len(cookiePath)] == '/'
}

func readCookieParamsForDomain(cookieStoreDir, domain string) ([]*proto.NetworkCookieParam, bool, error) {
	sessionPath, canonical, err := cookieSessionFilePath(cookieStoreDir, domain)
	if err != nil {
		return nil, false, err
	}
	mu := cookieFileMutex(sessionPath)
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(sessionPath)
	if err == nil {
		var session cookieSession
		if err := json.Unmarshal(data, &session); err != nil {
			if quarantineErr := quarantineCookieFile(sessionPath); quarantineErr != nil {
				return nil, false, fmt.Errorf("解析 cookie session 失败: %w; 隔离损坏文件失败: %v", err, quarantineErr)
			}
			log.Printf("[browser-cookie] quarantined corrupted cookie session path=%s err=%v", sessionPath, err)
			return nil, false, newCookieSessionUnavailableError(fmt.Errorf("cookie session 已损坏；请重新登录"))
		}
		if session.Version != cookieSessionSchemaVersion {
			return nil, false, newCookieSessionUnavailableError(fmt.Errorf("不支持的 cookie session 版本 %d", session.Version))
		}
		if session.Domain != canonical {
			return nil, false, newCookieSessionUnavailableError(fmt.Errorf("cookie session 域名不匹配: %s", session.Domain))
		}
		return session.Cookies, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("读取 cookie session 失败: %w", err)
	}

	// Backwards-compatible read-only fallback for pre-session installations.
	// It deliberately does not write: explicit browser login is the sole
	// production writer of session.json, so a background poll can never race a
	// newly completed login. The next login writes the canonical session and
	// retires these old snapshots.
	return readLegacyCookieSnapshots(cookieStoreDir, domain)
}

func readLegacyCookieSnapshots(cookieStoreDir, requestedDomain string) ([]*proto.NetworkCookieParam, bool, error) {
	requested, err := sanitizeCookieDomain(requestedDomain)
	if err != nil {
		return nil, false, err
	}
	canonical, err := canonicalCookieDomain(requested)
	if err != nil {
		return nil, false, err
	}
	domains, err := legacyCookieDomainsForSession(cookieStoreDir, requested, canonical)
	if err != nil {
		return nil, false, err
	}
	var snapshots [][]*proto.NetworkCookieParam
	for _, domain := range domains {
		path, err := cookieFilePath(cookieStoreDir, domain)
		if err != nil {
			return nil, false, err
		}
		cookies, loaded, err := readLegacyCookieParams(path)
		if err != nil {
			return nil, false, err
		}
		if loaded {
			snapshots = append(snapshots, cookies)
		}
	}
	if len(snapshots) == 0 {
		return nil, false, nil
	}
	return mergeCookieSnapshots(snapshots...), true, nil
}

func legacyCookieDomainsForSession(cookieStoreDir, requested, canonical string) ([]string, error) {
	ordered := append([]string{requested}, alternateCookieDomains(requested)...)
	entries, err := os.ReadDir(cookieStoreDir)
	if err != nil {
		if os.IsNotExist(err) {
			return uniqueCookieDomains(ordered), nil
		}
		return nil, fmt.Errorf("读取 legacy cookie 目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".session.json") {
			continue
		}
		domain := strings.TrimSuffix(entry.Name(), ".json")
		candidate, err := canonicalCookieDomain(domain)
		if err != nil || candidate != canonical {
			continue
		}
		ordered = append(ordered, domain)
	}
	return uniqueCookieDomains(ordered), nil
}

func uniqueCookieDomains(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	unique := make([]string, 0, len(domains))
	for _, domain := range domains {
		d, err := sanitizeCookieDomain(domain)
		if err != nil {
			continue
		}
		if _, exists := seen[d]; exists {
			continue
		}
		seen[d] = struct{}{}
		unique = append(unique, d)
	}
	return unique
}

// retireLegacyCookieSnapshots moves pre-session snapshots out of the import
// set once session.json exists. This keeps the migration one-way without a
// second persistent state machine: a later damaged or deleted session asks for
// a fresh login instead of silently reviving an old credential.
func retireLegacyCookieSnapshots(cookieStoreDir, canonical string) error {
	entries, err := os.ReadDir(cookieStoreDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 legacy cookie 目录失败: %w", err)
	}
	suffix := time.Now().Format("20060102T150405.000000000")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".session.json") {
			continue
		}
		domain := strings.TrimSuffix(entry.Name(), ".json")
		candidate, err := canonicalCookieDomain(domain)
		if err != nil || candidate != canonical {
			continue
		}
		path := filepath.Join(cookieStoreDir, entry.Name())
		archived := fmt.Sprintf("%s.legacy.%s.%d", path, suffix, os.Getpid())
		if err := os.Rename(path, archived); err != nil {
			return fmt.Errorf("归档 legacy cookie 文件失败: %w", err)
		}
	}
	return nil
}

func readLegacyCookieParams(cookiePath string) ([]*proto.NetworkCookieParam, bool, error) {
	mu := cookieFileMutex(cookiePath)
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(cookiePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("读取 legacy cookie 文件失败: %w", err)
	}
	var cookies []*proto.NetworkCookieParam
	if err := json.Unmarshal(data, &cookies); err != nil {
		if quarantineErr := quarantineCookieFile(cookiePath); quarantineErr != nil {
			return nil, false, fmt.Errorf("解析 legacy cookie 文件失败: %w; 隔离损坏文件失败: %v", err, quarantineErr)
		}
		log.Printf("[browser-cookie] quarantined corrupted legacy cookie file path=%s err=%v", cookiePath, err)
		return nil, false, nil
	}
	return cookies, true, nil
}

func mergeCookieSnapshots(snapshots ...[]*proto.NetworkCookieParam) []*proto.NetworkCookieParam {
	seen := make(map[string]struct{})
	merged := make([]*proto.NetworkCookieParam, 0)
	for _, cookies := range snapshots {
		for _, cookie := range cookies {
			if cookie == nil {
				continue
			}
			key := cookieScopeKey(cookie)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, cookie)
		}
	}
	return merged
}

func cookieScopeKey(cookie *proto.NetworkCookieParam) string {
	partitionSite := ""
	partitionCrossSite := ""
	if cookie.PartitionKey != nil {
		partitionSite = cookie.PartitionKey.TopLevelSite
		if cookie.PartitionKey.HasCrossSiteAncestor {
			partitionCrossSite = "1"
		}
	}
	return strings.Join([]string{cookie.Name, strings.ToLower(strings.TrimSpace(cookie.Domain)), strings.TrimSpace(cookie.Path), partitionSite, partitionCrossSite}, "\x00")
}

func alternateCookieDomains(domain string) []string {
	d, err := sanitizeCookieDomain(domain)
	if err != nil {
		return nil
	}
	if strings.HasPrefix(d, "www.") {
		return []string{strings.TrimPrefix(d, "www.")}
	}
	return []string{"www." + d}
}

func quarantineCookieFile(cookiePath string) error {
	suffix := time.Now().Format("20060102T150405.000000000")
	return os.Rename(cookiePath, fmt.Sprintf("%s.bad.%s.%d", cookiePath, suffix, os.Getpid()))
}

// saveDomainCookies is intentionally used only by InteractiveLogin. Normal
// Watchlist browsing reads the session but never mutates the user's login
// state.
func saveDomainCookies(page interface {
	Cookies(urls []string) ([]*proto.NetworkCookie, error)
}, domain, cookieStoreDir string, targetURL *url.URL) error {
	canonical, err := canonicalCookieDomain(domain)
	if err != nil {
		return err
	}
	cookies, err := collectSessionCookies(page, canonical, targetURL)
	if err != nil {
		return err
	}
	sessionPath, _, err := cookieSessionFilePath(cookieStoreDir, canonical)
	if err != nil {
		return err
	}
	mu := cookieFileMutex(sessionPath)
	mu.Lock()
	defer mu.Unlock()
	if err := writeCookieSessionLocked(sessionPath, canonical, cookies); err != nil {
		return err
	}
	return retireLegacyCookieSnapshots(cookieStoreDir, canonical)
}

func collectSessionCookies(page interface {
	Cookies(urls []string) ([]*proto.NetworkCookie, error)
}, canonical string, targetURL *url.URL) ([]*proto.NetworkCookieParam, error) {
	if rodPage, ok := page.(*rod.Page); ok {
		// Attachment setup may already be cancelled. Cookie collection belongs
		// to the current interactive login, just like other page operations.
		allCookies, err := rodPage.Browser().Context(rodPage.GetContext()).GetCookies()
		if err != nil {
			return nil, fmt.Errorf("读取浏览器 cookie session 失败: %w", err)
		}
		return cookieParamsFromNetwork(filterSessionCookies(allCookies, canonical), canonical), nil
	}
	cookies, err := page.Cookies([]string{targetURL.String()})
	if err != nil {
		return nil, fmt.Errorf("读取页面 cookie 失败: %w", err)
	}
	return cookieParamsFromNetwork(cookies, canonical), nil
}

func filterSessionCookies(cookies []*proto.NetworkCookie, canonical string) []*proto.NetworkCookie {
	filtered := make([]*proto.NetworkCookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cookie.Domain)), ".")
		if domain == canonical || strings.HasSuffix(domain, "."+canonical) {
			filtered = append(filtered, cookie)
		}
	}
	return filtered
}

func cookieParamsFromNetwork(cookies []*proto.NetworkCookie, fallbackDomain string) []*proto.NetworkCookieParam {
	params := make([]*proto.NetworkCookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		domain := cookie.Domain
		if domain == "" {
			domain = fallbackDomain
		}
		param := &proto.NetworkCookieParam{Name: cookie.Name, Value: cookie.Value, Domain: domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: cookie.SameSite, Expires: cookie.Expires, Priority: cookie.Priority, SameParty: cookie.SameParty, SourceScheme: cookie.SourceScheme}
		if cookie.SourcePort != 0 {
			port := cookie.SourcePort
			param.SourcePort = &port
		}
		if cookie.PartitionKey != nil {
			param.PartitionKey = &proto.NetworkCookiePartitionKey{TopLevelSite: cookie.PartitionKey.TopLevelSite, HasCrossSiteAncestor: cookie.PartitionKey.HasCrossSiteAncestor}
		}
		params = append(params, param)
	}
	return params
}

func writeCookieSessionLocked(path, domain string, cookies []*proto.NetworkCookieParam) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 cookie 目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cookieSession{Version: cookieSessionSchemaVersion, Domain: domain, UpdatedAt: time.Now().UTC(), Cookies: cookies}, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 cookie session 失败: %w", err)
	}
	if err := writeFileAtomicLocked(path, data, 0o600); err != nil {
		return fmt.Errorf("写入 cookie session 失败: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	mu := cookieFileMutex(path)
	mu.Lock()
	defer mu.Unlock()
	return writeFileAtomicLocked(path, data, perm)
}

func writeFileAtomicLocked(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
