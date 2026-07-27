package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	htmlmd "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
)

var cookieDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

// BrowserBrowseRequest 本地浏览器抓取请求
type BrowserBrowseRequest struct {
	URL             string `json:"url" jsonschema:"required,description=需要访问的目标网页 URL"`
	Action          string `json:"action,omitempty" jsonschema:"description=动作类型，支持 read 或 screenshot，默认 read"`
	Mode            string `json:"mode,omitempty" jsonschema:"description=提取模式：full（完整内容）、snapshot（快照模式，仅可交互元素）、summary（摘要模式），默认 full"`
	TabID           string `json:"tab_id,omitempty" jsonschema:"description=可选标签页ID，用于复用标签页"`
	WaitSelector    string `json:"wait_selector,omitempty" jsonschema:"description=可选 CSS 选择器，等待目标元素出现后再提取"`
	ContentSelector string `json:"content_selector,omitempty" jsonschema:"description=可选 CSS 选择器，指定提取内容的范围（mode=full时有效）"`
	UseCookieDomain string `json:"use_cookie_domain,omitempty" jsonschema:"description=可选 Cookie 域名（如 weixin.qq.com），用于加载并复用该域的 Cookie"`
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
}

// BrowserBrowse 使用本地 go-rod 浏览器读取动态页面或截图
func BrowserBrowse(ctx context.Context, req *BrowserBrowseRequest) (*BrowserBrowseResponse, error) {
	if req == nil || strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
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
	if err := checkAllowedDomain(targetURL.Hostname(), cfg.AllowedDomains); err != nil {
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

	if req.UseCookieDomain != "" {
		if err := checkAllowedDomain(req.UseCookieDomain, cfg.AllowedDomains); err != nil {
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

	title, _ := pageTitle(page)
	finalURL := targetURL.String()
	if info, err := page.Info(); err == nil && strings.TrimSpace(info.URL) != "" {
		finalURL = strings.TrimSpace(info.URL)
	}
	resp := &BrowserBrowseResponse{
		URL:   finalURL,
		Title: title,
	}

	switch action {
	case "screenshot":
		imgBytes, err := page.Screenshot(true, nil)
		if err != nil {
			return nil, fmt.Errorf("页面截图失败: %w", err)
		}
		resp.ScreenshotBase = base64.StdEncoding.EncodeToString(imgBytes)
	default:
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
				// 使用指定的选择器提取内容
				htmlContent, textContent, err = extractContentBySelector(page, req.ContentSelector, cfg.WaitSelectorTimeoutSec)
			} else {
				// 使用默认的正文提取逻辑
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

	if err := saveDomainCookies(page, cookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
		return resp, fmt.Errorf("页面抓取成功，但保存 Cookie 失败: %w", err)
	}

	return resp, nil
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

func loadDomainCookies(page interface {
	SetCookies(cookies []*proto.NetworkCookieParam) error
}, domain, cookieStoreDir string, targetURL *url.URL) error {
	cookiePath, err := cookieFilePath(cookieStoreDir, domain)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(cookiePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 cookie 文件失败: %w", err)
	}

	var cookies []*proto.NetworkCookieParam
	if err := json.Unmarshal(data, &cookies); err != nil {
		return fmt.Errorf("解析 cookie 文件失败: %w", err)
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
	if err := os.WriteFile(cookiePath, data, 0o600); err != nil {
		return fmt.Errorf("写入 cookie 文件失败: %w", err)
	}
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
