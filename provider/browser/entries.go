package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/ocr"
	"github.com/originaleric/digcrawl/protocol"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/policy"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/skill"
)

type browseInput struct {
	URL               string `json:"url"`
	Action            string `json:"action,omitempty"`
	Mode              string `json:"mode,omitempty"`
	TabID             string `json:"tab_id,omitempty"`
	WaitSelector      string `json:"wait_selector,omitempty"`
	ContentSelector   string `json:"content_selector,omitempty"`
	UseCookieDomain   string `json:"use_cookie_domain,omitempty"`
	IncludeScreenshot bool   `json:"include_screenshot,omitempty"`
	IncludeMedia      bool   `json:"include_media,omitempty"`
	IncludeOCR        bool   `json:"include_ocr,omitempty"`
}

var browsePage = browser.BrowserBrowse

type directBrowserOCRPolicy struct {
	requested  bool
	skillOwned bool
	rootConfig config.OCRConfig
}

// resolveDirectBrowserOCR keeps browser.browse on the same Skill policy as
// platform readers, while retaining the root OCR fallback used by standalone
// gateway configurations. An explicit include_ocr remains Skill-owned so a
// caller receives the existing opt-in validation when Skill OCR is disabled.
func resolveDirectBrowserOCR(ctx context.Context, skillCfg config.SkillConfig, rootCfg config.OCRConfig, explicitlyRequested bool) directBrowserOCRPolicy {
	if skill.DirectReadOCRRequested(ctx, skillCfg, explicitlyRequested) {
		return directBrowserOCRPolicy{requested: true, skillOwned: true}
	}
	if !executor.IsCollectExecution(ctx) && rootCfg.EnabledOrDefault() {
		return directBrowserOCRPolicy{requested: true, rootConfig: rootCfg}
	}
	return directBrowserOCRPolicy{}
}

// BrowseEntry returns registry entry for browser.browse.
func BrowseEntry(configDomains []string, artStore artifact.Store, runtimeConfigs ...*config.Config) registry.Entry {
	runtimeCfg := config.Get()
	if len(runtimeConfigs) > 0 && runtimeConfigs[0] != nil {
		runtimeCfg = runtimeConfigs[0]
	}
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        ToolBrowserBrowse,
			Description: "Open a URL in a local browser and extract rendered content.",
			InputSchema: registry.MustSchema(map[string]any{
				"type":     "object",
				"required": []string{"url"},
				"properties": map[string]any{
					"url":                map[string]string{"type": "string"},
					"action":             map[string]string{"type": "string"},
					"mode":               map[string]string{"type": "string"},
					"wait_selector":      map[string]string{"type": "string"},
					"content_selector":   map[string]string{"type": "string"},
					"use_cookie_domain":  map[string]string{"type": "string"},
					"include_screenshot": map[string]any{"type": "boolean"},
					"include_media":      map[string]any{"type": "boolean"},
					"include_ocr":        map[string]any{"type": "boolean"},
				},
			}),
			Capabilities: []string{"browser", "web.read", "cookie.local"},
			Risk:         "network",
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			in, err := executor.DecodeInput[browseInput](call)
			if err != nil {
				return nil, nil, err
			}
			if err := validateCallURL(in.URL, call, configDomains); err != nil {
				return nil, nil, err
			}
			if err := executor.ValidateCookieDomain(in.UseCookieDomain, call, configDomains); err != nil {
				return nil, nil, err
			}
			contentSelector := in.ContentSelector
			if strings.TrimSpace(contentSelector) == "" && (strings.TrimSpace(in.Mode) == "" || strings.EqualFold(strings.TrimSpace(in.Mode), "full")) {
				contentSelector = "body"
			}
			var skillCfg config.SkillConfig
			var imageDownloadCfg config.ImageDownloadConfig
			if runtimeCfg == config.Get() {
				skillCfg, imageDownloadCfg = config.SkillImageDownload()
			} else {
				skillCfg, imageDownloadCfg = runtimeCfg.Skill, runtimeCfg.ImageDownload
			}
			isCollect := executor.IsCollectExecution(ctx)
			ocrPolicy := resolveDirectBrowserOCR(ctx, skillCfg, runtimeCfg.OCR, in.IncludeOCR)
			retainProviderImages := imageDownloadCfg.EnabledFor("web")
			returnMedia := in.IncludeMedia
			if !isCollect && in.IncludeOCR {
				returnMedia = true
			}
			resp, err := browsePage(ctx, &browser.BrowserBrowseRequest{
				URL: in.URL, Action: in.Action, CaptureScreenshot: in.IncludeScreenshot, Mode: in.Mode, TabID: in.TabID,
				WaitSelector: in.WaitSelector, ContentSelector: contentSelector,
				UseCookieDomain: in.UseCookieDomain, MetadataScript: browserImageMetadataScript(),
			})
			if err != nil {
				return nil, nil, err
			}
			out := map[string]any{"source_url": resp.URL, "title": resp.Title}
			if resp.Text != "" {
				out["text"] = resp.Text
			}
			if resp.Markdown != "" {
				out["markdown"] = resp.Markdown
			}
			if resp.TabID != "" {
				out["tab_id"] = resp.TabID
			}
			if resp.Summary != "" {
				out["summary"] = resp.Summary
			}
			if returnMedia || ocrPolicy.requested || retainProviderImages {
				if media := browserImageMedia(resp.Metadata); len(media) > 0 {
					out["media"] = media
				}
			}
			if !returnMedia && !isCollect {
				// Direct OCR needs the resolved image references during processing,
				// but its local default must not alter browser.browse's text-only
				// response contract.
				defer delete(out, "media")
			}
			var localImages []skill.LocalImageInput
			if retainProviderImages {
				inputs, retainErr := platformprov.RetainResolvedProviderImages(ctx, out, imageDownloadCfg, artStore)
				if retainErr != nil {
					if !returnMedia && !isCollect {
						delete(out, "media")
					}
					return out, nil, retainErr
				}
				if !isCollect {
					localImages = inputs
				}
			}
			if !isCollect && ocrPolicy.requested && ocrPolicy.skillOwned {
				if err := skill.Enhance(ctx, out, skillCfg, skill.EnhanceOptions{
					IncludeOCR: true, ScreenshotBase64: resp.ScreenshotBase, ArtifactStore: artStore, LocalImages: localImages,
				}); err != nil {
					return out, nil, err
				}
			}
			if !isCollect && ocrPolicy.requested && !ocrPolicy.skillOwned {
				opts := ocr.EnrichOptions{}
				if resp.ScreenshotBase != "" {
					opts.ExtraImages = []ocr.ImageRef{ocr.PNGDataURLImageRef("screenshot", resp.ScreenshotBase)}
				}
				ocr.EnrichMap(ctx, out, ocrPolicy.rootConfig, opts)
			}
			if retainProviderImages && !returnMedia && !isCollect {
				delete(out, "media")
			}
			var artifacts []protocol.Artifact
			if resp.ScreenshotBase != "" {
				artID := artifact.NewID()
				if artStore == nil {
					return out, nil, fmt.Errorf("screenshot artifact store is unavailable")
				}
				art, err := artifact.PutBase64PNG(ctx, artStore, artID, resp.ScreenshotBase, runtimeCfg.Gateway.ArtifactMaxInlineBytes)
				if err != nil {
					return out, nil, fmt.Errorf("persist screenshot artifact: %w", err)
				}
				artifacts = append(artifacts, art)
				out["screenshot_artifact_id"] = art.ID
			}
			return out, artifacts, nil
		},
	}
}

const browserImageMetadataScriptSource = `() => ({ image_urls: [...new Set(Array.from(document.images).filter(img => { const r = img.getBoundingClientRect(); return r.width > 0 && r.height > 0; }).map(img => { try { const u = new URL(img.currentSrc || img.src, location.href); return /^https?:$/.test(u.protocol) && !u.username && !u.password ? u.href : ''; } catch (_) { return ''; } }).filter(Boolean))] })`

func browserImageMetadataScript() string { return browserImageMetadataScriptSource }

func browserImageMedia(meta map[string]any) []map[string]any {
	if meta == nil {
		return nil
	}
	values, _ := meta["image_urls"].([]any)
	if len(values) == 0 {
		if strings, ok := meta["image_urls"].([]string); ok {
			for _, value := range strings {
				values = append(values, value)
			}
		}
	}
	seen := map[string]struct{}{}
	media := make([]map[string]any, 0, len(values))
	for _, value := range values {
		rawURL, _ := value.(string)
		rawURL = strings.TrimSpace(rawURL)
		parsed, err := url.Parse(rawURL)
		if rawURL == "" || err != nil || parsed.User != nil || parsed.Host == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
			continue
		}
		if _, ok := seen[rawURL]; ok {
			continue
		}
		seen[rawURL] = struct{}{}
		media = append(media, map[string]any{"type": "image", "url": rawURL})
	}
	return media
}

// SnapshotEntry returns registry entry for browser.snapshot.
func SnapshotEntry(configDomains []string) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        ToolBrowserSnapshot,
			Description: "Capture a structured accessibility snapshot of a page with interactive elements.",
			InputSchema: registry.MustSchema(map[string]any{
				"type":     "object",
				"required": []string{"url"},
				"properties": map[string]any{
					"url":               map[string]string{"type": "string"},
					"filter":            map[string]string{"type": "string"},
					"max_depth":         map[string]any{"type": "integer"},
					"wait_selector":     map[string]string{"type": "string"},
					"use_cookie_domain": map[string]string{"type": "string"},
				},
			}),
			Capabilities: []string{"browser", "web.read", "cookie.local"},
			Risk:         "network",
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			in, err := executor.DecodeInput[browser.BrowserSnapshotRequest](call)
			if err != nil {
				return nil, nil, err
			}
			if err := validateCallURL(in.URL, call, configDomains); err != nil {
				return nil, nil, err
			}
			if err := executor.ValidateCookieDomain(in.UseCookieDomain, call, configDomains); err != nil {
				return nil, nil, err
			}
			resp, err := browser.BrowserSnapshot(ctx, &in)
			if err != nil {
				return nil, nil, err
			}
			return map[string]any{
				"source_url": resp.URL,
				"title":      resp.Title,
				"elements":   resp.Elements,
			}, nil, nil
		},
	}
}

// ActionEntry returns registry entry for browser.action.
func ActionEntry(configDomains []string) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        ToolBrowserAction,
			Description: "Perform browser interactions (click, type, scroll, etc.) on a page.",
			InputSchema: registry.MustSchema(map[string]any{
				"type":     "object",
				"required": []string{"url", "action"},
				"properties": map[string]any{
					"url":               map[string]string{"type": "string"},
					"ref":               map[string]string{"type": "string"},
					"selector":          map[string]string{"type": "string"},
					"action":            map[string]string{"type": "string"},
					"text":              map[string]string{"type": "string"},
					"key":               map[string]string{"type": "string"},
					"use_cookie_domain": map[string]string{"type": "string"},
				},
			}),
			Capabilities:         []string{"browser", "web.interact", "cookie.local"},
			Risk:                 "network",
			RequiresUserApproval: true,
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			in, err := executor.DecodeInput[browser.BrowserActionRequest](call)
			if err != nil {
				return nil, nil, err
			}
			if err := validateCallURL(in.URL, call, configDomains); err != nil {
				return nil, nil, err
			}
			if err := executor.ValidateCookieDomain(in.UseCookieDomain, call, configDomains); err != nil {
				return nil, nil, err
			}
			resp, err := browser.BrowserAction(ctx, &in)
			if err != nil {
				return nil, nil, err
			}
			return map[string]any{
				"success": resp.Success,
				"message": resp.Message,
				"url":     resp.URL,
			}, nil, nil
		},
	}
}

func validateCallURL(rawURL string, call *protocol.ToolCall, configDomains []string) error {
	domains := policy.MergeDomains(&call.Policy, configDomains)
	return policy.ValidateURLDomain(rawURL, domains, nil)
}
