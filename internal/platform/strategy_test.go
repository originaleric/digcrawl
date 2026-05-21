package platform

import "testing"

func TestClassifyAPIResponseLogin(t *testing.T) {
	t.Parallel()
	a := ClassifyAPIResponse("xiaohongshu", 401, nil, nil)
	if a.OK || !a.Fatal || a.FallbackBrowser || a.ErrorCode != ErrLoginRequired {
		t.Fatalf("unexpected: %+v", a)
	}
}

func TestClassifyAPIResponseEmpty(t *testing.T) {
	t.Parallel()
	a := ClassifyAPIResponse("douyin", 200, map[string]any{"aweme_detail": map[string]any{}}, func(m map[string]any) bool {
		d, _ := m["aweme_detail"].(map[string]any)
		return len(d) == 0
	})
	if a.OK || !a.FallbackBrowser {
		t.Fatalf("unexpected: %+v", a)
	}
}

func TestIsAPIFallback(t *testing.T) {
	t.Parallel()
	err := NewAPIFallbackError("test")
	if _, ok := IsAPIFallback(err); !ok {
		t.Fatal("expected fallback")
	}
}
