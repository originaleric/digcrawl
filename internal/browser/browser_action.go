package browser

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
)

// BrowserActionRequest 浏览器操作请求
type BrowserActionRequest struct {
	URL             string `json:"url" jsonschema:"required,description=目标网页 URL（需要先导航到此页面）"`
	Ref             string `json:"ref,omitempty" jsonschema:"description=元素引用ID（如 e0, e1, e2），优先使用"`
	Selector        string `json:"selector,omitempty" jsonschema:"description=CSS 选择器，当ref未提供时使用"`
	Action          string `json:"action" jsonschema:"required,description=操作类型：click, type, fill, hover, scroll, focus, press"`
	Text            string `json:"text,omitempty" jsonschema:"description=输入文本（type/fill操作需要）"`
	Key             string `json:"key,omitempty" jsonschema:"description=按键名称（press操作需要，如 Enter, Tab, Escape）"`
	ScrollX         int    `json:"scroll_x,omitempty" jsonschema:"description=滚动X偏移（scroll操作）"`
	ScrollY         int    `json:"scroll_y,omitempty" jsonschema:"description=滚动Y偏移（scroll操作）"`
	HumanLike       bool   `json:"human_like,omitempty" jsonschema:"description=是否模拟人类操作（随机延迟）"`
	UseCookieDomain string `json:"use_cookie_domain,omitempty" jsonschema:"description=可选 Cookie 域名"`
}

// BrowserActionResponse 浏览器操作响应
type BrowserActionResponse struct {
	Success bool   `json:"success" jsonschema:"description=操作是否成功"`
	Message string `json:"message,omitempty" jsonschema:"description=操作结果消息"`
	URL     string `json:"url,omitempty" jsonschema:"description=当前页面URL"`
}

// BrowserAction 执行浏览器操作
func BrowserAction(ctx context.Context, req *BrowserActionRequest) (*BrowserActionResponse, error) {
	if req == nil || strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	if strings.TrimSpace(req.Action) == "" {
		return nil, fmt.Errorf("action 不能为空")
	}

	cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
	action := strings.ToLower(strings.TrimSpace(req.Action))

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TotalTimeoutSec)*time.Second)
	defer cancel()

	manager := getBrowserManager()
	session, err := manager.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer manager.release(session)

	page := session.page.Timeout(time.Duration(cfg.TotalTimeoutSec) * time.Second)

	// 加载Cookie（如果指定）
	if req.UseCookieDomain != "" {
		targetURL, err := validateURL(req.URL)
		if err != nil {
			return nil, err
		}
		if err := checkAllowedDomain(req.UseCookieDomain, cfg.AllowedDomains); err != nil {
			return nil, fmt.Errorf("cookie 域名不在白名单: %w", err)
		}
		if err := loadDomainCookies(page, req.UseCookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
			return nil, err
		}
	}

	// 导航到目标URL（如果当前不在该页面）
	var currentURLStr string
	currentURL, _ := page.Eval("window.location.href")
	if currentURL != nil {
		// 使用 ObjectToJSON 获取值
		if jsonVal, err := page.ObjectToJSON(currentURL); err == nil {
			currentURLStr = jsonVal.Str()
		}
	}
	if !strings.Contains(currentURLStr, req.URL) {
		if err := page.Timeout(time.Duration(cfg.NavigateTimeoutSec) * time.Second).Navigate(req.URL); err != nil {
			return nil, fmt.Errorf("页面导航失败: %w", err)
		}
		if err := page.WaitLoad(); err != nil {
			return nil, fmt.Errorf("页面加载失败: %w", err)
		}
	}

	// 模拟人类延迟
	if req.HumanLike {
		humanDelay()
	}

	// 执行操作
	var actionErr error
	switch action {
	case "click":
		actionErr = executeClick(page, req)
	case "type":
		actionErr = executeType(page, req)
	case "fill":
		actionErr = executeFill(page, req)
	case "hover":
		actionErr = executeHover(page, req)
	case "scroll":
		actionErr = executeScroll(page, req)
	case "focus":
		actionErr = executeFocus(page, req)
	case "press":
		actionErr = executePress(page, req)
	default:
		return nil, fmt.Errorf("不支持的操作类型: %s，支持的操作：click, type, fill, hover, scroll, focus, press", action)
	}

	if actionErr != nil {
		return &BrowserActionResponse{
			Success: false,
			Message: fmt.Sprintf("操作失败: %v", actionErr),
		}, nil
	}

	currentURLResult, _ := page.Eval("window.location.href")
	urlStr := ""
	if currentURLResult != nil {
		// 使用 ObjectToJSON 获取值
		if jsonVal, err := page.ObjectToJSON(currentURLResult); err == nil {
			urlStr = jsonVal.Str()
		}
	}

	return &BrowserActionResponse{
		Success: true,
		Message: fmt.Sprintf("操作 %s 执行成功", action),
		URL:     urlStr,
	}, nil
}

// executeClick 执行点击操作
func executeClick(page *rod.Page, req *BrowserActionRequest) error {
	if req.HumanLike {
		humanDelay()
	}

	if req.Ref != "" {
		// 通过引用操作（需要先获取快照）
		return fmt.Errorf("通过ref操作需要先调用browser_snapshot获取元素引用")
	}

	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err != nil {
			return fmt.Errorf("找不到元素: %w", err)
		}
		return el.Click(proto.InputMouseButtonLeft, 1)
	}

	return fmt.Errorf("需要提供 ref 或 selector")
}

// executeType 执行输入操作
func executeType(page *rod.Page, req *BrowserActionRequest) error {
	if req.Text == "" {
		return fmt.Errorf("type 操作需要 text 参数")
	}

	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err != nil {
			return fmt.Errorf("找不到元素: %w", err)
		}
		if req.HumanLike {
			return humanType(el, req.Text)
		}
		return el.Input(req.Text)
	}

	return fmt.Errorf("需要提供 selector")
}

// executeFill 执行填充操作（清空后输入）
func executeFill(page *rod.Page, req *BrowserActionRequest) error {
	if req.Text == "" {
		return fmt.Errorf("fill 操作需要 text 参数")
	}

	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err != nil {
			return fmt.Errorf("找不到元素: %w", err)
		}
		// 清空现有内容
		if err := el.SelectAllText(); err == nil {
			_ = el.Input("")
		}
		if req.HumanLike {
			return humanType(el, req.Text)
		}
		return el.Input(req.Text)
	}

	return fmt.Errorf("需要提供 selector")
}

// executeHover 执行悬停操作
func executeHover(page *rod.Page, req *BrowserActionRequest) error {
	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err != nil {
			return fmt.Errorf("找不到元素: %w", err)
		}
		return el.Hover()
	}

	return fmt.Errorf("需要提供 selector")
}

// executeScroll 执行滚动操作
func executeScroll(page *rod.Page, req *BrowserActionRequest) error {
	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err != nil {
			return fmt.Errorf("找不到元素: %w", err)
		}
		return el.ScrollIntoView()
	}

	// 滚动到指定位置
	scrollX := req.ScrollX
	scrollY := req.ScrollY
	if scrollX == 0 && scrollY == 0 {
		scrollY = 800 // 默认向下滚动
	}

	js := fmt.Sprintf("window.scrollBy(%d, %d)", scrollX, scrollY)
	_, err := page.Eval(js)
	return err
}

// executeFocus 执行聚焦操作
func executeFocus(page *rod.Page, req *BrowserActionRequest) error {
	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err != nil {
			return fmt.Errorf("找不到元素: %w", err)
		}
		return el.Focus()
	}

	return fmt.Errorf("需要提供 selector")
}

// executePress 执行按键操作
func executePress(page *rod.Page, req *BrowserActionRequest) error {
	if req.Key == "" {
		return fmt.Errorf("press 操作需要 key 参数")
	}

	// 如果有selector，先聚焦元素
	if req.Selector != "" {
		el, err := page.Element(req.Selector)
		if err == nil {
			_ = el.Focus()
		}
	}

	// 将按键名称转换为 input.Key
	keyLower := strings.ToLower(req.Key)

	// 检查是否是特殊按键
	switch keyLower {
	case "enter":
		return page.Keyboard.Type(input.Enter)
	case "tab":
		return page.Keyboard.Type(input.Tab)
	case "escape":
		return page.Keyboard.Type(input.Escape)
	case "backspace":
		return page.Keyboard.Type(input.Backspace)
	case "delete":
		return page.Keyboard.Type(input.Delete)
	case "arrowup", "up":
		return page.Keyboard.Type(input.ArrowUp)
	case "arrowdown", "down":
		return page.Keyboard.Type(input.ArrowDown)
	case "arrowleft", "left":
		return page.Keyboard.Type(input.ArrowLeft)
	case "arrowright", "right":
		return page.Keyboard.Type(input.ArrowRight)
	default:
		// 对于普通字符，使用 Element.Input 方法输入文本
		if req.Selector != "" {
			el, err := page.Element(req.Selector)
			if err != nil {
				return fmt.Errorf("找不到元素: %w", err)
			}
			if err := el.Focus(); err != nil {
				return fmt.Errorf("聚焦元素失败: %w", err)
			}
			return el.Input(req.Key)
		}
		// 如果没有 selector，返回错误（需要指定 selector 才能输入文本）
		return fmt.Errorf("press 操作需要 selector 参数才能输入普通文本")
	}
}

// humanDelay 模拟人类操作的随机延迟
func humanDelay() {
	// 随机延迟 100-500ms
	delay := time.Duration(100+rand.Intn(400)) * time.Millisecond
	time.Sleep(delay)
}

// humanType 模拟人类输入（逐字符输入，带随机延迟）
func humanType(el *rod.Element, text string) error {
	for _, char := range text {
		if err := el.Input(string(char)); err != nil {
			return err
		}
		humanDelay()
	}
	return nil
}
