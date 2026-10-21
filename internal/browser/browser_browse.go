package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	htmlmd "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/runtime/policy"
)

type browserSessionRunner func(context.Context, *BrowserBrowseRequest, *browserSession) (*BrowserBrowseResponse, error)

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
	// Internal options preserve browser.snapshot's existing filter/depth contract.
	snapshotFilter   string
	snapshotMaxDepth int
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
	Diagnostics    map[string]any `json:"browser_diagnostics,omitempty"`
	MediaURLs      []string       `json:"media_urls,omitempty" jsonschema:"description=浏览器探测到的 HTTP 媒体资源地址"`
	snapshotRefs   map[string]int64
}

// BrowserBrowse 使用本地 go-rod 浏览器读取动态页面或截图
func BrowserBrowse(ctx context.Context, req *BrowserBrowseRequest) (resp *BrowserBrowseResponse, retErr error) {
	return browserBrowseWithSessionRunner(ctx, req, getBrowserManager, nil)
}

// browserBrowseWithSessionRunner holds the request-level recovery boundary.
// The optional runner is only used by focused lifecycle tests; production
// executes the page operations below with the singleton manager.
func browserBrowseWithSessionRunner(ctx context.Context, req *BrowserBrowseRequest, managerProvider func() *browserManager, runner browserSessionRunner) (*BrowserBrowseResponse, error) {
	ctx, state, cancel := browseInvocationContext(ctx)
	defer cancel()
	replaying := false
	for {
		state.beginAttempt(replaying)
		resp, err := browserBrowseAttempt(ctx, req, managerProvider, runner)
		// Cleanup is independently bounded and can outlive the request. Do not
		// publish a success if cancellation happened during that final step.
		if err == nil && ctx.Err() != nil {
			err = newBrowseError(ErrorBrowserTimeout, "read_page", "浏览器读取已取消或时间预算耗尽", ctx.Err(), false)
		}
		if err == nil {
			state.succeeded()
			if resp != nil {
				resp.Diagnostics = BrowseDiagnostics(ctx)
			}
			recordBrowseDiagnostic(ctx, err)
			return resp, nil
		}
		retry := state.claimRetry(ctx, err, replaying)
		if !retry {
			err = finishBrowseError(ctx, err)
			recordBrowseDiagnostic(ctx, err)
			return nil, err
		}
		replaying = true
		if !state.waitRetry(ctx) {
			err = finishBrowseError(ctx, err)
			recordBrowseDiagnostic(ctx, err)
			return nil, err
		}
	}
}

func browserBrowseAttempt(ctx context.Context, req *BrowserBrowseRequest, managerProvider func() *browserManager, runner browserSessionRunner) (resp *BrowserBrowseResponse, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil || strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	rootCfg := config.Get()
	cfg := normalizeLocalBrowserConfig(rootCfg.LocalBrowser)
	sourceDomains := rootCfg.Gateway.AllowedDomains
	if req.snapshotFilter != "" {
		// Preserve the dedicated snapshot API's existing local-browser policy.
		sourceDomains = cfg.AllowedDomains
	}
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
	if err := policy.ValidateURLDomain(targetURL.String(), sourceDomains, nil); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TotalTimeoutSec)*time.Second)
	defer cancel()

	if managerProvider == nil {
		return nil, fmt.Errorf("browser manager provider is unavailable")
	}
	manager := managerProvider()
	if manager == nil {
		return nil, fmt.Errorf("browser manager is unavailable")
	}
	acquireStart := time.Now()
	session, err := manager.acquireSession(ctx)
	var connection *browserConnection
	if session != nil {
		connection = session.conn
	}
	recordBrowserStage(ctx, "acquire", connection, time.Since(acquireStart), err)
	if err != nil {
		return nil, newBrowseError(ErrorBrowserUnavailable, "acquire", "无法启动或连接本地浏览器", err, false)
	}
	defer func() {
		if isConnectionFailure(retErr) {
			manager.retireConnection(session.conn)
		}
		if err := manager.releaseSession(session); err != nil && (retErr == nil || browseCauseReason(err) == "external_target_cleanup_required") {
			resp = nil
			retErr = newBrowseError(ErrorBrowserUnavailable, "close_page", "浏览器页面清理失败", err, false)
		}
	}()

	if runner != nil {
		return runner(ctx, req, session)
	}

	// Bind every CDP operation to the caller's context. A page returned by the
	// pool otherwise keeps the browser-level context, so a tool timeout can
	// expire while WaitLoad/Element keeps blocking on a page that has already
	// navigated away or closed.
	page := session.page.Context(ctx).Timeout(time.Duration(cfg.TotalTimeoutSec) * time.Second)
	var finishMediaCapture func() []string
	if req.CaptureMedia {
		finishMediaCapture, err = startMediaResponseCapture(ctx, page)
		if err != nil {
			return nil, newBrowseError(ErrorBrowserContentNotFound, "capture_media", "初始化媒体探测失败", err, false)
		}
		defer func() {
			if finishMediaCapture != nil {
				finishMediaCapture()
			}
		}()
	}

	if req.UseCookieDomain != "" {
		if err := policy.ValidateURLDomain("https://"+req.UseCookieDomain, sourceDomains, nil); err != nil {
			return nil, fmt.Errorf("cookie 域名不在白名单: %w", err)
		}
		if err := loadDomainCookies(page, req.UseCookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
			return nil, newBrowseError(ErrorBrowserCookieFailed, "load_cookies", "加载本地浏览器 Cookie 失败", err, false)
		}
	}

	navigateStart := time.Now()
	if err := page.Timeout(time.Duration(cfg.NavigateTimeoutSec) * time.Second).Navigate(targetURL.String()); err != nil {
		recordBrowserStage(ctx, "navigate", session.conn, time.Since(navigateStart), err)
		return nil, newBrowseError(ErrorBrowserNavigationFailed, "navigate", "页面导航失败", err, true)
	}
	recordBrowserStage(ctx, "navigate", session.conn, time.Since(navigateStart), nil)
	loadStart := time.Now()
	loadErr := page.WaitLoad()
	recordBrowserStage(ctx, "wait_load", session.conn, time.Since(loadStart), loadErr)
	if loadErr != nil && (isLocalBrowserFailure(ctx.Err(), loadErr) || isConnectionFailure(loadErr)) {
		return nil, newBrowseError(ErrorBrowserPageLoadFailed, "wait_load", "页面加载失败", loadErr, true)
	}
	if err := loadErr; err != nil {
		// Modern video and SPA pages can replace the initial document while the
		// load event is pending. If CDP can still inspect the page, let the
		// platform-specific selector wait decide DOM readiness instead of
		// discarding a usable page solely because the load event was interrupted.
		if _, infoErr := (proto.TargetGetTargetInfo{TargetID: page.TargetID}).Call(page.Browser().Context(ctx)); infoErr != nil {
			return nil, newBrowseError(ErrorBrowserPageLoadFailed, "wait_load", "无法确认加载中断后的页面状态", infoErr, true)
		}
		log.Printf("[browser] load event interrupted; page remains inspectable, continuing with DOM readiness check: %s", browseCauseReason(err))
	}
	if req.WaitSelector != "" {
		selectorStart := time.Now()
		_, err := page.Timeout(time.Duration(cfg.WaitSelectorTimeoutSec) * time.Second).Element(req.WaitSelector)
		recordBrowserStage(ctx, "wait_selector", session.conn, time.Since(selectorStart), err)
		if err != nil {
			return nil, newBrowseError(ErrorBrowserSelectorTimeout, "wait_selector", "等待页面内容选择器失败", err, true)
		}
	}

	// Rod's Page.Info uses the browser context retained during attachment.
	// That setup context has ended; browser-level calls need the read context.
	info, infoErr := (proto.TargetGetTargetInfo{TargetID: page.TargetID}).Call(page.Browser().Context(ctx))
	if infoErr != nil {
		return nil, newBrowseError(ErrorBrowserUnavailable, "read_page", "获取页面信息失败", infoErr, false)
	}
	if info.TargetInfo == nil || strings.TrimSpace(info.TargetInfo.URL) == "" {
		return nil, newBrowseError(ErrorBrowserContentNotFound, "read_page", "无法确认页面实际地址", fmt.Errorf("target info has no URL"), false)
	}
	finalURL := strings.TrimSpace(info.TargetInfo.URL)
	if err := policy.ValidateURLDomain(finalURL, sourceDomains, nil); err != nil {
		return nil, err
	}
	// Keep the main document identity local to this attempt. URL comparison
	// detects SPA navigation; the loader also detects a reload at the same URL.
	frame, frameErr := (proto.PageGetFrameTree{}).Call(page)
	if frameErr != nil {
		return nil, newBrowseError(ErrorBrowserUnavailable, "read_page", "确认页面文档失败", frameErr, false)
	}
	if frame.FrameTree == nil || frame.FrameTree.Frame == nil || frame.FrameTree.Frame.LoaderID == "" {
		return nil, newBrowseError(ErrorBrowserContentNotFound, "read_page", "无法确认页面文档", nil, false)
	}
	title := strings.TrimSpace(info.TargetInfo.Title)
	resp = &BrowserBrowseResponse{
		URL:   finalURL,
		Title: title,
	}
	readStart := time.Now()
	defer func() { recordBrowserStage(ctx, "read_page", session.conn, time.Since(readStart), retErr) }()

	if action != "screenshot" {
		// 根据 mode 选择不同的提取方式
		switch mode {
		case "snapshot":
			// 快照模式：仅返回可交互元素
			elements, refs, err := FetchAXTree(ctx, page)
			if err != nil {
				return nil, newBrowseError(ErrorBrowserContentNotFound, "snapshot", "获取页面快照失败", err, false)
			}
			filter := req.snapshotFilter
			if filter == "" {
				filter = FilterInteractive
			}
			resp.Elements = filterElements(elements, filter)
			if req.snapshotMaxDepth > 0 {
				filtered := make([]AXNode, 0, len(resp.Elements))
				for _, node := range resp.Elements {
					if node.Depth <= req.snapshotMaxDepth {
						filtered = append(filtered, node)
					}
				}
				resp.Elements = filtered
			}
			resp.snapshotRefs = refs
		case "summary":
			// 摘要模式：提取关键信息
			summary, err := extractSummary(page, cfg.WaitSelectorTimeoutSec)
			if err != nil {
				return nil, newBrowseError(ErrorBrowserContentNotFound, "summary", "提取页面摘要失败", err, false)
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
				return nil, newBrowseError(ErrorBrowserContentNotFound, "extract_content", "提取页面正文失败", err, false)
			}
			converter := htmlmd.NewConverter("", true, nil)
			md, err := converter.ConvertString(htmlContent)
			if err != nil {
				return nil, newBrowseError(ErrorBrowserContentConversion, "convert_markdown", "页面正文转换为 Markdown 失败", err, false)
			}
			resp.Text = strings.TrimSpace(textContent)
			resp.Markdown = strings.TrimSpace(md)
		}
	}
	if action == "screenshot" || req.CaptureScreenshot {
		imgBytes, err := page.Screenshot(false, nil)
		if err != nil {
			return nil, newBrowseError(ErrorBrowserScreenshotFailed, "screenshot", "页面截图失败", err, true)
		}
		resp.ScreenshotBase = base64.StdEncoding.EncodeToString(imgBytes)
	}

	if script := strings.TrimSpace(req.MetadataScript); script != "" {
		meta, err := evalPageMetadata(page, script)
		if err == nil && meta == nil {
			err = fmt.Errorf("metadata script returned null instead of an object")
		}
		if err != nil {
			// Platform scripts also detect login/captcha states; missing metadata
			// cannot safely be treated as a complete read of the extracted body.
			return nil, newBrowseError(ErrorBrowserContentNotFound, "metadata", "提取页面结构化信息失败", err, false)
		}
		resp.Metadata = meta
	}
	if req.CaptureMedia {
		// Keep navigation-time Media responses as a fallback. Some players fetch
		// their primary stream before their video element is attached, so resetting
		// here would discard the only playable URL.
		captured, err := captureMediaURLs(page)
		if err != nil {
			return nil, newBrowseError(ErrorBrowserContentNotFound, "capture_media", "探测页面媒体失败", err, false)
		}
		if finishMediaCapture != nil {
			captured = append(captured, finishMediaCapture()...)
			finishMediaCapture = nil
		}
		resp.MediaURLs = uniqueMediaURLs(captured)
	}

	// Extraction and requested enrichment can trigger another SPA navigation.
	// Revalidate the actual source before publishing the completed read.
	info, infoErr = (proto.TargetGetTargetInfo{TargetID: page.TargetID}).Call(page.Browser().Context(ctx))
	if infoErr != nil {
		return nil, newBrowseError(ErrorBrowserUnavailable, "read_page", "确认页面最终地址失败", infoErr, false)
	}
	if info.TargetInfo == nil || strings.TrimSpace(info.TargetInfo.URL) == "" {
		return nil, newBrowseError(ErrorBrowserContentNotFound, "read_page", "无法确认页面最终地址", fmt.Errorf("target info has no URL"), false)
	}
	if err := policy.ValidateURLDomain(info.TargetInfo.URL, sourceDomains, nil); err != nil {
		return nil, err
	}
	finalFrame, frameErr := (proto.PageGetFrameTree{}).Call(page)
	if frameErr != nil {
		return nil, newBrowseError(ErrorBrowserUnavailable, "read_page", "确认页面最终文档失败", frameErr, false)
	}
	if finalFrame.FrameTree == nil || finalFrame.FrameTree.Frame == nil || finalFrame.FrameTree.Frame.LoaderID == "" {
		return nil, newBrowseError(ErrorBrowserContentNotFound, "read_page", "无法确认页面最终文档", nil, false)
	}
	if strings.TrimSpace(info.TargetInfo.URL) != finalURL ||
		frame.FrameTree.Frame.URL+frame.FrameTree.Frame.URLFragment != finalURL ||
		finalFrame.FrameTree.Frame.URL+finalFrame.FrameTree.Frame.URLFragment != finalURL ||
		frame.FrameTree.Frame.ID != finalFrame.FrameTree.Frame.ID ||
		frame.FrameTree.Frame.LoaderID != finalFrame.FrameTree.Frame.LoaderID ||
		frame.FrameTree.Frame.URL != finalFrame.FrameTree.Frame.URL ||
		frame.FrameTree.Frame.URLFragment != finalFrame.FrameTree.Frame.URLFragment {
		e := newBrowseError(ErrorBrowserContentNotFound, "read_page", "读取期间页面已切换，请等待页面稳定后重新读取", nil, false).(*BrowseError)
		e.Diagnostics = map[string]any{"operation": "read_page", "reason": "page_changed"}
		return nil, e
	}
	resp.Title = strings.TrimSpace(info.TargetInfo.Title)
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

func captureMediaURLs(page *rod.Page) ([]string, error) {
	if page == nil {
		return nil, fmt.Errorf("media probe page is nil")
	}
	result, err := page.Eval(mediaResourceProbeScript)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("media probe returned nil")
	}
	raw, err := page.ObjectToJSON(result)
	if err != nil {
		return nil, err
	}
	var urls []string
	// ObjectToJSON returns the JSON array itself. Str formats arrays as Go
	// display text ([url ...]), which is not JSON once a URL is present.
	if err := raw.Unmarshal(&urls); err != nil {
		return nil, fmt.Errorf("invalid media probe result: %w", err)
	}
	if urls == nil {
		return nil, fmt.Errorf("media probe returned null instead of an array")
	}
	return urls, nil
}

func startMediaResponseCapture(ctx context.Context, page *rod.Page) (func() []string, error) {
	if page == nil {
		return nil, fmt.Errorf("media capture page is nil")
	}
	probeCtx, cancel := context.WithCancel(ctx)
	probePage := page.Context(probeCtx)
	if err := (proto.NetworkEnable{}).Call(probePage); err != nil {
		cancel()
		return nil, err
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
	return finish, nil
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
			return nil, fmt.Errorf("metadata value string is not a JSON object")
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
			return nil, fmt.Errorf("metadata value is not a JSON object: type=%T err=%w", rawValue, err)
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
	// A live parent's optional selector timeout is a normal miss. Transport,
	// queue and cancellation failures must stop before trying another selector.
	for _, selector := range selectors {
		el, err := page.Timeout(timeout).Element(selector)
		if err != nil {
			if page.GetContext().Err() != nil || isLocalBrowserFailure(nil, err) || isConnectionFailure(err) || (isRetryableBrowserCause(err) && !errors.Is(err, context.DeadlineExceeded)) {
				return "", "", fmt.Errorf("未能提取页面正文: %w", err)
			}
			continue
		}
		htmlContent, err := el.HTML()
		if err != nil {
			if isRetryableBrowserCause(err) {
				return "", "", fmt.Errorf("读取 HTML 失败: %w", err)
			}
			continue
		}
		textContent, err := el.Text()
		if err != nil {
			if isRetryableBrowserCause(err) {
				return "", "", fmt.Errorf("读取文本失败: %w", err)
			}
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
	title, err := pageTitle(page.Timeout(time.Duration(waitSelectorTimeoutSec) * time.Second))
	if err != nil && (page.GetContext().Err() != nil || isLocalBrowserFailure(nil, err) || isConnectionFailure(err) || (isRetryableBrowserCause(err) && !errors.Is(err, context.DeadlineExceeded))) {
		return "", err
	}
	if err == nil && title != "" {
		summary.WriteString(fmt.Sprintf("标题: %s\n\n", title))
	}

	// 提取 meta description
	metaDesc, err := page.Eval(`document.querySelector('meta[name="description"]')?.content || ''`)
	if isRetryableBrowserCause(err) {
		return "", err
	}
	if metaDesc != nil {
		// 使用 ObjectToJSON 获取值
		if jsonVal, err := page.ObjectToJSON(metaDesc); err == nil {
			desc := jsonVal.Str()
			if desc != "" {
				summary.WriteString(fmt.Sprintf("描述: %s\n\n", desc))
			}
		} else if isRetryableBrowserCause(err) {
			return "", err
		}
	}

	// 提取前几个段落或列表项
	selectors := []string{"p", "li", "h1", "h2", "h3"}
	for _, selector := range selectors {
		elements, err := page.Elements(selector)
		if err != nil {
			if isRetryableBrowserCause(err) {
				return "", err
			}
			continue
		}
		count := 0
		for _, el := range elements {
			if count >= 5 { // 最多提取5个元素
				break
			}
			text, err := el.Text()
			if isRetryableBrowserCause(err) {
				return "", err
			}
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
