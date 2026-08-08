package browser

import (
	"context"
	"encoding/base64"
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

	htmlmd "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/runtime/policy"
)

var cookieDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
var cookieFileLocks sync.Map

// BrowserBrowseRequest 本地浏览器抓取请求
type BrowserBrowseRequest struct {
	URL               string `json:"url" jsonschema:"required,description=需要访问的目标网页 URL"`
	Action            string `json:"action,omitempty" jsonschema:"description=动作类型，支持 read 或 screenshot（当前视口），默认 read"`
	CaptureScreenshot bool   `json:"capture_screenshot,omitempty" jsonschema:"description=读取正文时额外返回当前视口截图"`
	Mode              string `json:"mode,omitempty" jsonschema:"description=提取模式：full（完整内容）、snapshot（快照模式，仅可交互元素）、summary（摘要模式），默认 full"`
	TabID             string `json:"tab_id,omitempty" jsonschema:"description=可选标签页ID，用于复用标签页"`
	WaitSelector      string `json:"wait_selector,omitempty" jsonschema:"description=可选 CSS 选择器，等待目标元素出现后再提取"`
	ContentSelector   string `json:"content_selector,omitempty" jsonschema:"description=可选 CSS 选择器，指定提取内容的范围（mode=full时有效）"`
	UseCookieDomain   string `json:"use_cookie_domain,omitempty" jsonschema:"description=可选 Cookie 域名（如 weixin.qq.com），用于加载并复用该域的 Cookie"`
	// CaptureMedia asks the local browser to discover playable HTTP media resources.
	// It is opt-in because probing a player can start playback and add a short wait.
	CaptureMedia bool `json:"capture_media,omitempty" jsonschema:"description=是否探测页面播放媒体资源，仅视频读取链路按需启用"`
	// MetadataScript 可选：页面加载后执行的 JS 表达式，应返回可 JSON 序列化的对象（平台结构化字段提取）。
	MetadataScript string `json:"metadata_script,omitempty" jsonschema:"description=可选 JS 表达式，返回 JSON 对象作为 metadata"`
}

// BrowserBrowseResponse 本地浏览器抓取响应
type BrowserBrowseResponse struct {
	URL            string         `json:"url" jsonschema:"description=最终抓取的 URL"`
	Title          string         `json:"title" jsonschema:"description=页面标题"`
	TabID          string         `json:"tab_id,omitempty" jsonschema:"description=标签页ID（可用于后续操作复用）"`
	Text           string         `json:"text,omitempty" jsonschema:"description=提取的正文纯文本"`
	Markdown       string         `json:"markdown,omitempty" jsonschema:"description=转换后的正文 Markdown"`
	ScreenshotBase string         `json:"screenshot_base64,omitempty" jsonschema:"description=页面截图 base64（action=screenshot 时返回）"`
	Elements       []AXNode       `json:"elements,omitempty" jsonschema:"description=可交互元素列表（mode=snapshot时返回）"`
	Summary        string         `json:"summary,omitempty" jsonschema:"description=页面摘要（mode=summary时返回）"`
	Metadata       map[string]any `json:"metadata,omitempty" jsonschema:"description=MetadataScript 提取的结构化字段"`
	MediaURLs      []string       `json:"media_urls,omitempty" jsonschema:"description=浏览器探测到的 HTTP 媒体资源地址"`
}

// BrowserBrowse 使用本地 go-rod 浏览器读取动态页面或截图
func BrowserBrowse(ctx context.Context, req *BrowserBrowseRequest) (*BrowserBrowseResponse, error) {
	if req == nil || strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	rootCfg := config.Get()
	cfg := normalizeLocalBrowserConfig(rootCfg.LocalBrowser)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "read"
	}
	if action != "read" && action != "screenshot" {
		return nil, fmt.Errorf("action 仅支持 read 或 screenshot")
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "full"
	}
	if mode != "full" && mode != "snapshot" && mode != "summary" {
		return nil, fmt.Errorf("mode 仅支持 full、snapshot 或 summary")
	}

	targetURL, err := validateURL(req.URL)
	if err != nil {
		return nil, err
	}
	if err := policy.ValidateURLDomain(targetURL.String(), rootCfg.Gateway.AllowedDomains, nil); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TotalTimeoutSec)*time.Second)
	defer cancel()

	manager := getBrowserManager()
	session, err := manager.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer manager.release(session)

	page := session.page.Timeout(time.Duration(cfg.TotalTimeoutSec) * time.Second)
	var finishMediaCapture func() []string
	if req.CaptureMedia {
		finishMediaCapture = startMediaResponseCapture(ctx, page)
	}

	if req.UseCookieDomain != "" {
		if err := policy.ValidateURLDomain("https://"+req.UseCookieDomain, rootCfg.Gateway.AllowedDomains, nil); err != nil {
			return nil, fmt.Errorf("cookie 域名不在白名单: %w", err)
		}
		if err := loadDomainCookies(page, req.UseCookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
			return nil, err
		}
	}

	if err := page.Timeout(time.Duration(cfg.NavigateTimeoutSec) * time.Second).Navigate(targetURL.String()); err != nil {
		return nil, fmt.Errorf("页面导航失败: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("页面加载失败: %w", err)
	}
	if req.WaitSelector != "" {
		if _, err := page.Timeout(time.Duration(cfg.WaitSelectorTimeoutSec) * time.Second).Element(req.WaitSelector); err != nil {
			return nil, fmt.Errorf("等待选择器失败: %w", err)
		}
	}

	finalURL := targetURL.String()
	if info, err := page.Info(); err == nil && strings.TrimSpace(info.URL) != "" {
		finalURL = strings.TrimSpace(info.URL)
	}
	if err := policy.ValidateURLDomain(finalURL, rootCfg.Gateway.AllowedDomains, nil); err != nil {
		return nil, err
	}
	title, _ := pageTitle(page)
	resp := &BrowserBrowseResponse{
		URL:   finalURL,
		Title: title,
	}

	if action != "screenshot" {
		// 根据 mode 选择不同的提取方式
		switch mode {
		case "snapshot":
			// 快照模式：仅返回可交互元素
			elements, _, err := FetchAXTree(ctx, page)
			if err != nil {
				return nil, fmt.Errorf("获取快照失败: %w", err)
			}
			// 过滤仅可交互元素
			resp.Elements = filterElements(elements, FilterInteractive)
		case "summary":
			// 摘要模式：提取关键信息
			summary, err := extractSummary(page, cfg.WaitSelectorTimeoutSec)
			if err != nil {
				return nil, fmt.Errorf("提取摘要失败: %w", err)
			}
			resp.Summary = summary
		default:
			// full 模式：完整内容提取
			var htmlContent, textContent string
			var err error

			if req.ContentSelector != "" {
				htmlContent, textContent, err = extractContentBySelector(page, req.ContentSelector, cfg.WaitSelectorTimeoutSec)
			} else {
				htmlContent, textContent, err = extractMainContent(page, cfg.WaitSelectorTimeoutSec)
			}

			if err != nil {
				return nil, err
			}
			converter := htmlmd.NewConverter("", true, nil)
			md, err := converter.ConvertString(htmlContent)
			if err != nil {
				return nil, fmt.Errorf("HTML 转 Markdown 失败: %w", err)
			}
			resp.Text = strings.TrimSpace(textContent)
			resp.Markdown = strings.TrimSpace(md)
		}
	}
	if action == "screenshot" || req.CaptureScreenshot {
		imgBytes, err := page.Screenshot(false, nil)
		if err != nil {
			return nil, fmt.Errorf("页面截图失败: %w", err)
		}
		resp.ScreenshotBase = base64.StdEncoding.EncodeToString(imgBytes)
	}

	cookieDomain := strings.TrimSpace(req.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = targetURL.Hostname()
	}
	if script := strings.TrimSpace(req.MetadataScript); script != "" {
		if meta, err := evalPageMetadata(page, script); err == nil && len(meta) > 0 {
			resp.Metadata = meta
		} else if err != nil {
			resp.Metadata = map[string]any{"_metadata_error": err.Error()}
		}
	}
	if req.CaptureMedia {
		// Keep navigation-time Media responses as a fallback. Some players fetch
		// their primary stream before their video element is attached, so resetting
		// here would discard the only playable URL.
		captured := captureMediaURLs(page)
		if finishMediaCapture != nil {
			captured = append(captured, finishMediaCapture()...)
		}
		resp.MediaURLs = uniqueMediaURLs(captured)
	}

	if err := saveDomainCookies(page, cookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
		return resp, fmt.Errorf("页面抓取成功，但保存 Cookie 失败: %w", err)
	}

	return resp, nil
}

const mediaResourceProbeScript = `async () => {
  const sleep = (ms) => new Promise(resolve => setTimeout(resolve, ms));
  const uniq = (items) => [...new Set(items.map(v => String(v || '').trim()).filter(Boolean))];
  const normalize = (raw) => {
    try {
      const u = new URL(String(raw || '').replaceAll('\\/', '/').replaceAll('\\u0026', '&'), location.href);
      return /^https?:$/.test(u.protocol) ? u.href : '';
    } catch (_) {
      return '';
    }
  };
  const isMediaURL = (raw) => {
    const u = normalize(raw);
    if (!u) return false;
    const value = u.toLowerCase();
    return /\.(?:mp4|m3u8|mpd|webm|mov)(?:[?#]|$)/.test(value)
      || /(?:[?&](?:mime_type|media_type|content_type)=video(?:[_%2f-]|$))/.test(value);
  };
  const visibleByArea = (selector) => Array.from(document.querySelectorAll(selector)).filter(element => {
    const rect = element.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  }).sort((a, b) => {
    const ar = a.getBoundingClientRect();
    const br = b.getBoundingClientRect();
    return br.width * br.height - ar.width * ar.height;
  });
  const videos = () => visibleByArea('video');
  const primaryVideo = () => videos()[0];
  const primaryPlayer = () => primaryVideo()
    || visibleByArea('[class*="player" i], [class*="video" i], [data-e2e*="player" i], [data-e2e*="video" i]')[0];
  const player = primaryPlayer();
  if (!player) return [];
  try {
    player.click();
  } catch (_) {}
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const video = primaryVideo();
    if (video) {
      try {
        video.muted = true;
        await video.play();
      } catch (_) {}
      const videoURLs = [
        video.currentSrc, video.src, video.getAttribute('src'),
        ...Array.from(video.querySelectorAll('source')).map(source => source.src || source.getAttribute('src'))
      ];
      // A URL sourced directly from a <video> element is playable even when
      // its CDN path intentionally omits a .mp4/.m3u8 extension.
      const direct = uniq(videoURLs.map(normalize).filter(Boolean));
      if (direct.length) return direct.slice(0, 8);
    }
    await sleep(500);
  }
  return [];
}`

func captureMediaURLs(page *rod.Page) []string {
	if page == nil {
		return nil
	}
	result, err := page.Eval(mediaResourceProbeScript)
	if err != nil || result == nil {
		return nil
	}
	raw, err := page.ObjectToJSON(result)
	if err != nil {
		return nil
	}
	var urls []string
	if err := json.Unmarshal([]byte(raw.Str()), &urls); err != nil {
		return nil
	}
	return urls
}

func startMediaResponseCapture(ctx context.Context, page *rod.Page) func() []string {
	if page == nil {
		return func() []string { return nil }
	}
	probeCtx, cancel := context.WithCancel(ctx)
	probePage := page.Context(probeCtx)
	if err := (proto.NetworkEnable{}).Call(probePage); err != nil {
		cancel()
		return func() []string { return nil }
	}
	seen := make(map[string]struct{})
	var urls []string
	var mu sync.Mutex
	done := make(chan struct{})
	wait := probePage.EachEvent(func(event *proto.NetworkResponseReceived) {
		if event == nil || event.Response == nil || !isVideoMediaResponse(event) {
			return
		}
		if u := strings.TrimSpace(event.Response.URL); u != "" {
			mu.Lock()
			if _, ok := seen[u]; !ok {
				seen[u] = struct{}{}
				urls = append(urls, u)
			}
			mu.Unlock()
		}
	})
	go func() {
		defer close(done)
		wait()
	}()
	finish := func() []string {
		cancel()
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), urls...)
	}
	return finish
}

func isVideoMediaResponse(event *proto.NetworkResponseReceived) bool {
	if event == nil || event.Response == nil {
		return false
	}
	if event.Type == proto.NetworkResourceTypeMedia {
		return true
	}
	mimeType := strings.ToLower(strings.TrimSpace(event.Response.MIMEType))
	return strings.HasPrefix(mimeType, "video/") || strings.Contains(mimeType, "mpegurl") || strings.Contains(mimeType, "dash+xml")
}

func uniqueMediaURLs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func evalPageMetadata(page *rod.Page, script string) (map[string]any, error) {
	result, err := page.Eval(script)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("metadata script returned nil")
	}
	if rawValue := result.Value.Raw(); rawValue != nil {
		if rawString, ok := rawValue.(string); ok {
			var meta map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(rawString)), &meta); err == nil {
				return meta, nil
			}
			return nil, fmt.Errorf("metadata value string is not a JSON object: %s", truncateForError(rawString, 120))
		}
		if rawBytes, ok := rawValue.([]byte); ok {
			var meta map[string]any
			if err := json.Unmarshal(rawBytes, &meta); err != nil {
				return nil, fmt.Errorf("metadata value bytes is not a JSON object: %w", err)
			}
			return meta, nil
		}
		data, err := json.Marshal(rawValue)
		if err != nil {
			return nil, fmt.Errorf("metadata value is not JSON: %w", err)
		}
		var meta map[string]any
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("metadata value is not a JSON object: type=%T value=%s err=%w", rawValue, truncateForError(string(data), 120), err)
		}
		return meta, nil
	}
	jsonVal, err := page.ObjectToJSON(result)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(jsonVal.Str())
	if raw == "" {
		return nil, fmt.Errorf("metadata script returned empty")
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("metadata is not a JSON object: %w", err)
	}
	return meta, nil
}

func truncateForError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func validateURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("无效 url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("仅支持 http/https URL")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("url 缺少 host")
	}
	return u, nil
}

func checkAllowedDomain(host string, allowedDomains []string) error {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return fmt.Errorf("空域名")
	}
	if len(allowedDomains) == 0 {
		return nil
	}
	for _, domain := range allowedDomains {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		if h == d || strings.HasSuffix(h, "."+d) {
			return nil
		}
	}
	return fmt.Errorf("目标域名 %s 不在允许列表", host)
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

func cookieFilePath(cookieStoreDir, domain string) (string, error) {
	d, err := sanitizeCookieDomain(domain)
	if err != nil {
		return "", err
	}
	return filepath.Join(cookieStoreDir, d+".json"), nil
}

func cookieFileMutex(cookiePath string) *sync.Mutex {
	key := filepath.Clean(cookiePath)
	mu, _ := cookieFileLocks.LoadOrStore(key, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func loadDomainCookies(page interface {
	SetCookies(cookies []*proto.NetworkCookieParam) error
}, domain, cookieStoreDir string, targetURL *url.URL) error {
	cookiePath, err := cookieFilePath(cookieStoreDir, domain)
	if err != nil {
		return err
	}

	cookies, loaded, err := readCookieParams(cookiePath)
	if err != nil {
		return err
	}
	if !loaded {
		for _, alternate := range alternateCookieDomains(domain) {
			alternatePath, err := cookieFilePath(cookieStoreDir, alternate)
			if err != nil {
				continue
			}
			if filepath.Clean(alternatePath) == filepath.Clean(cookiePath) {
				continue
			}
			cookies, loaded, err = readCookieParams(alternatePath)
			if err != nil {
				return err
			}
			if loaded {
				log.Printf("[browser-cookie] loaded fallback cookie domain=%s path=%s", alternate, alternatePath)
				break
			}
		}
	}
	for _, c := range cookies {
		if c.URL == "" {
			c.URL = targetURL.Scheme + "://" + targetURL.Host
		}
	}
	if len(cookies) == 0 {
		return nil
	}
	if err := page.SetCookies(cookies); err != nil {
		return fmt.Errorf("注入 cookie 失败: %w", err)
	}
	return nil
}

func readCookieParams(cookiePath string) ([]*proto.NetworkCookieParam, bool, error) {
	mu := cookieFileMutex(cookiePath)
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(cookiePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("读取 cookie 文件失败: %w", err)
	}

	var cookies []*proto.NetworkCookieParam
	if err := json.Unmarshal(data, &cookies); err != nil {
		if quarantineErr := quarantineCookieFile(cookiePath); quarantineErr != nil {
			return nil, false, fmt.Errorf("解析 cookie 文件失败: %w; 隔离损坏 cookie 文件失败: %v", err, quarantineErr)
		}
		log.Printf("[browser-cookie] quarantined corrupted cookie file path=%s err=%v", cookiePath, err)
		return nil, false, nil
	}
	return cookies, true, nil
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
	target := fmt.Sprintf("%s.bad.%s.%d", cookiePath, suffix, os.Getpid())
	return os.Rename(cookiePath, target)
}

func saveDomainCookies(page interface {
	Cookies(urls []string) ([]*proto.NetworkCookie, error)
}, domain, cookieStoreDir string, targetURL *url.URL) error {
	safeDomain, err := sanitizeCookieDomain(domain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cookieStoreDir, 0o755); err != nil {
		return fmt.Errorf("创建 cookie 目录失败: %w", err)
	}

	cookies, err := page.Cookies([]string{targetURL.String()})
	if err != nil {
		return fmt.Errorf("读取页面 cookie 失败: %w", err)
	}

	params := make([]*proto.NetworkCookieParam, 0, len(cookies))
	for _, c := range cookies {
		domainValue := c.Domain
		if domainValue == "" {
			domainValue = safeDomain
		}
		params = append(params, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domainValue,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: c.SameSite,
			Expires:  c.Expires,
		})
	}

	data, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 cookie 失败: %w", err)
	}
	cookiePath := filepath.Join(cookieStoreDir, safeDomain+".json")
	if err := writeFileAtomic(cookiePath, data, 0o600); err != nil {
		return fmt.Errorf("写入 cookie 文件失败: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	mu := cookieFileMutex(path)
	mu.Lock()
	defer mu.Unlock()

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

func pageTitle(page *rod.Page) (string, error) {
	el, err := page.Element("title")
	if err != nil {
		return "", err
	}
	return el.Text()
}

func extractMainContent(page *rod.Page, waitSelectorTimeoutSec int) (string, string, error) {
	selectors := []string{"#js_content", "article", "main", "[role='main']", "body"}
	timeout := time.Duration(waitSelectorTimeoutSec) * time.Second
	for _, selector := range selectors {
		el, err := page.Timeout(timeout).Element(selector)
		if err != nil {
			continue
		}
		htmlContent, err := el.HTML()
		if err != nil {
			continue
		}
		textContent, err := el.Text()
		if err != nil {
			continue
		}
		if strings.TrimSpace(textContent) == "" {
			continue
		}
		return htmlContent, textContent, nil
	}
	return "", "", fmt.Errorf("未能提取页面正文")
}

// extractContentBySelector 通过指定选择器提取内容
func extractContentBySelector(page *rod.Page, selector string, waitSelectorTimeoutSec int) (string, string, error) {
	timeout := time.Duration(waitSelectorTimeoutSec) * time.Second
	el, err := page.Timeout(timeout).Element(selector)
	if err != nil {
		return "", "", fmt.Errorf("找不到选择器 %s: %w", selector, err)
	}
	htmlContent, err := el.HTML()
	if err != nil {
		return "", "", fmt.Errorf("读取 HTML 失败: %w", err)
	}
	textContent, err := el.Text()
	if err != nil {
		return "", "", fmt.Errorf("读取文本失败: %w", err)
	}
	return htmlContent, textContent, nil
}

// extractSummary 提取页面摘要（简化版本，提取标题和关键信息）
func extractSummary(page *rod.Page, waitSelectorTimeoutSec int) (string, error) {
	var summary strings.Builder

	// 提取标题
	title, err := pageTitle(page)
	if err == nil && title != "" {
		summary.WriteString(fmt.Sprintf("标题: %s\n\n", title))
	}

	// 提取 meta description
	metaDesc, _ := page.Eval(`document.querySelector('meta[name="description"]')?.content || ''`)
	if metaDesc != nil {
		// 使用 ObjectToJSON 获取值
		if jsonVal, err := page.ObjectToJSON(metaDesc); err == nil {
			desc := jsonVal.Str()
			if desc != "" {
				summary.WriteString(fmt.Sprintf("描述: %s\n\n", desc))
			}
		}
	}

	// 提取前几个段落或列表项
	selectors := []string{"p", "li", "h1", "h2", "h3"}
	for _, selector := range selectors {
		elements, err := page.Elements(selector)
		if err != nil {
			continue
		}
		count := 0
		for _, el := range elements {
			if count >= 5 { // 最多提取5个元素
				break
			}
			text, err := el.Text()
			if err == nil && strings.TrimSpace(text) != "" {
				summary.WriteString(fmt.Sprintf("- %s\n", strings.TrimSpace(text)))
				count++
			}
		}
		if count > 0 {
			break
		}
	}

	result := summary.String()
	if result == "" {
		return "", fmt.Errorf("未能提取页面摘要")
	}
	return result, nil
}
