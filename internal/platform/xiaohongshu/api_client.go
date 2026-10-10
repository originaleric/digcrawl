package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/session"
)

const feedAPIURL = "https://edith.xiaohongshu.com/api/sns/web/v1/feed"

type feedRequest struct {
	SourceNoteID string         `json:"source_note_id"`
	ImageFormats []string       `json:"image_formats"`
	Extra        map[string]any `json:"extra"`
	XsecSource   string         `json:"xsec_source"`
	XsecToken    string         `json:"xsec_token"`
}

// FetchNoteViaAPI 通过浏览器上下文调用小红书 feed API。
func FetchNoteViaAPI(ctx context.Context, norm *platform.NormalizedURL, cookieDomain string) (map[string]any, *platform.APIAttempt, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		return nil, &platform.APIAttempt{
			FallbackBrowser: true,
			Message:         "missing note id",
		}, platform.NewAPIFallbackError("missing note id")
	}
	xsecToken := ""
	xsecSource := "pc_search"
	if norm.QueryParams != nil {
		xsecToken = norm.QueryParams["xsec_token"]
		if s := norm.QueryParams["xsec_source"]; s != "" {
			xsecSource = s
		}
	}
	body, err := json.Marshal(feedRequest{
		SourceNoteID: norm.PlatformID,
		ImageFormats: []string{"jpg", "webp", "avif"},
		Extra:        map[string]any{"need_body_topic": 1},
		XsecSource:   xsecSource,
		XsecToken:    xsecToken,
	})
	if err != nil {
		return nil, nil, err
	}

	warmURL := norm.EffectiveBrowseURL()
	if warmURL == "" {
		warmURL = "https://www.xiaohongshu.com"
	}
	result, err := session.FetchJSON(ctx, &browser.PageFetchRequest{
		WarmURL:         warmURL,
		FetchURL:        feedAPIURL,
		Method:          "POST",
		Body:            string(body),
		UseCookieDomain: cookieDomain,
		Referer:         warmURL,
	})
	if err != nil {
		if stopErr, stop := platform.ShouldStopStrategy(err); stop {
			return nil, &platform.APIAttempt{Fatal: true, Message: stopErr.Error()}, stopErr
		}
		return nil, &platform.APIAttempt{
			FallbackBrowser: true,
			Message:         err.Error(),
		}, platform.NewAPIFallbackError(err.Error())
	}

	attempt := platform.ClassifyAPIResponse(PlatformName, result.Status, result.ParsedJSON, xhsFeedEmpty)
	attempt.Meta["api_fetch_mode"] = "browser_context_fetch"
	attempt.Meta["api_signature_mode"] = "page_runtime"
	if !attempt.OK {
		return nil, attempt, attempt.Error()
	}
	return result.ParsedJSON, attempt, nil
}

func xhsFeedEmpty(parsed map[string]any) bool {
	items, ok := parsed["items"].([]any)
	if !ok || len(items) == 0 {
		return true
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		return true
	}
	if card, ok := first["note_card"].(map[string]any); ok && len(card) > 0 {
		return false
	}
	if card, ok := first["noteCard"].(map[string]any); ok && len(card) > 0 {
		return false
	}
	return true
}

// MapFeedResponse 将 feed API JSON 映射为 Content（extraction_source=api）。
func MapFeedResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed map[string]any) (*platform.Content, error) {
	card := extractNoteCard(parsed)
	if card == nil {
		return nil, fmt.Errorf("feed response missing note_card")
	}
	return mapNoteCard(in, norm, card), nil
}

func extractNoteCard(parsed map[string]any) map[string]any {
	items, ok := parsed["items"].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		return nil
	}
	if card, ok := first["note_card"].(map[string]any); ok {
		return card
	}
	if card, ok := first["noteCard"].(map[string]any); ok {
		return card
	}
	return nil
}
