package browser

import (
	"context"

	"github.com/originaleric/diggate/internal/browser"
	"github.com/originaleric/diggate/protocol"
	"github.com/originaleric/diggate/runtime/artifact"
	"github.com/originaleric/diggate/runtime/executor"
	"github.com/originaleric/diggate/runtime/policy"
	"github.com/originaleric/diggate/runtime/registry"
)

type browseInput struct {
	URL             string `json:"url"`
	Action          string `json:"action,omitempty"`
	Mode            string `json:"mode,omitempty"`
	TabID           string `json:"tab_id,omitempty"`
	WaitSelector    string `json:"wait_selector,omitempty"`
	ContentSelector string `json:"content_selector,omitempty"`
	UseCookieDomain string `json:"use_cookie_domain,omitempty"`
}

// BrowseEntry returns registry entry for browser.browse.
func BrowseEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        ToolBrowserBrowse,
			Description: "Open a URL in a local browser and extract rendered content.",
			InputSchema: registry.MustSchema(map[string]any{
				"type":     "object",
				"required": []string{"url"},
				"properties": map[string]any{
					"url":               map[string]string{"type": "string"},
					"action":            map[string]string{"type": "string"},
					"mode":              map[string]string{"type": "string"},
					"wait_selector":     map[string]string{"type": "string"},
					"content_selector":  map[string]string{"type": "string"},
					"use_cookie_domain": map[string]string{"type": "string"},
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
			resp, err := browser.BrowserBrowse(ctx, &browser.BrowserBrowseRequest{
				URL: in.URL, Action: in.Action, Mode: in.Mode, TabID: in.TabID,
				WaitSelector: in.WaitSelector, ContentSelector: in.ContentSelector,
				UseCookieDomain: in.UseCookieDomain,
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
			var artifacts []protocol.Artifact
			if resp.ScreenshotBase != "" {
				artID := call.ID + "_screenshot"
				if artStore != nil {
					art, err := artifact.PutBase64PNG(ctx, artStore, artID, resp.ScreenshotBase)
					if err == nil {
						artifacts = append(artifacts, art)
						out["screenshot_artifact_id"] = art.ID
					}
				} else {
					out["screenshot_artifact_id"] = artID
				}
			}
			return out, artifacts, nil
		},
	}
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
