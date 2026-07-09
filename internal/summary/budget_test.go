package summary

import (
	"strings"
	"testing"
)

func TestApplyBudgetPreservesIdentityFields(t *testing.T) {
	req := Request{
		WatchID: "w1", Platform: "rss", SourceName: "source", RunTime: "now",
		Items: []Item{{
			ItemID: "i1", URL: "https://example.com/a", Title: "title", PublishedAt: "2026-07-09",
			Text: strings.Repeat("正文", 200), ASRText: strings.Repeat("语音", 200), OCRText: "same\nsame\nprice 10",
			Metadata: map[string]any{"likes": 10, "raw": strings.Repeat("x", 1000)},
		}},
	}
	out := ApplyBudget(req, InputBudget{TextChars: 20, ASRChars: 20, OCRChars: 20, MetadataChars: 30, MaxInputChars: 500})
	item := out.Items[0]
	if item.URL != req.Items[0].URL || item.Title != req.Items[0].Title || item.PublishedAt != req.Items[0].PublishedAt {
		t.Fatalf("identity fields changed: %+v", item)
	}
	if len([]rune(item.Text)) > 25 || len([]rune(item.ASRText)) > 25 {
		t.Fatalf("text budget not applied: text=%d asr=%d", len([]rune(item.Text)), len([]rune(item.ASRText)))
	}
	if _, ok := item.Metadata["raw"]; ok {
		t.Fatalf("raw metadata leaked: %+v", item.Metadata)
	}
}
