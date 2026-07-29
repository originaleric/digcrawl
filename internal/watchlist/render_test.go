package watchlist

import (
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/internal/summary"
)

func TestRenderNotificationTrimsProviderLongText(t *testing.T) {
	longSummary := strings.Repeat("这是一段不应该被完整发送到飞书的正文。", 80)
	longHighlight := strings.Repeat("过长亮点", 60)
	msg := RenderNotification(&summary.Result{
		DigestTitle:    strings.Repeat("超长标题", 30),
		OverallSummary: longSummary,
		Items: []summary.ResultItem{{
			Title:      strings.Repeat("超长条目标题", 30),
			URL:        "https://example.com/a",
			Summary:    longSummary,
			Highlights: []string{longHighlight, "第二点", "第三点", "第四点"},
			Importance: "high",
		}},
		RecommendedAction: longSummary,
	})
	if strings.Contains(msg.Text, strings.Repeat("这是一段不应该被完整发送到飞书的正文。", 20)) {
		t.Fatalf("notification should not contain long copied body: %d chars", len([]rune(msg.Text)))
	}
	if !strings.Contains(msg.Text, "...(截断)") {
		t.Fatalf("expected truncated marker in notification: %s", msg.Text)
	}
	if strings.Contains(msg.Text, "第四点") {
		t.Fatalf("notification should cap highlights: %s", msg.Text)
	}
	if len([]rune(msg.Title)) > maxNotificationTitleRunes {
		t.Fatalf("title too long: %d", len([]rune(msg.Title)))
	}
}
