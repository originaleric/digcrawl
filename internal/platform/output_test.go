package platform

import "testing"

func TestApplyFormats(t *testing.T) {
	t.Parallel()
	c := &Content{
		Platform:    "xiaohongshu",
		ContentType: "note",
		SourceURL:   "https://www.xiaohongshu.com/explore/1",
		Text:        "body",
		Markdown:    "# title",
		CapturedAt:  "2026-05-19T10:00:00+08:00",
	}
	out := ApplyFormats(c, []string{"text"})
	if _, ok := out["markdown"]; ok {
		t.Fatalf("markdown should be omitted: %+v", out)
	}
	if out["text"] != "body" {
		t.Fatalf("expected text, got %+v", out)
	}
}

func TestEngagementFromMeta(t *testing.T) {
	t.Parallel()
	e := EngagementFromMeta(map[string]any{"likes": float64(10), "comments": float64(2)})
	if e == nil || e.Likes != 10 || e.Comments != 2 {
		t.Fatalf("unexpected engagement: %+v", e)
	}
}
