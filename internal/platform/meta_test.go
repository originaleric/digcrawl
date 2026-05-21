package platform

import "testing"

func TestEngagementFromMetaNumeric(t *testing.T) {
	t.Parallel()
	e := EngagementFromMeta(map[string]any{
		"likes":    float64(12000),
		"comments": 88,
		"reposts":  int64(3),
	})
	if e == nil || e.Likes != 12000 || e.Comments != 88 || e.Reposts != 3 {
		t.Fatalf("engagement: %+v", e)
	}
}

func TestIsFatalErrorCode(t *testing.T) {
	t.Parallel()
	if !IsFatalErrorCode(ErrLoginRequired) {
		t.Fatal("login should be fatal")
	}
	if IsFatalErrorCode(ErrPartialContent) {
		t.Fatal("partial should not be fatal")
	}
}

func TestErrorOutput(t *testing.T) {
	t.Parallel()
	out := ErrorOutput(NewReadError(ErrLoginRequired, "需要登录", "xiaohongshu", map[string]any{"k": "v"}))
	meta, ok := out["platform_metadata"].(map[string]any)
	if !ok || meta["error_code"] != ErrLoginRequired {
		t.Fatalf("out: %+v", out)
	}
}
