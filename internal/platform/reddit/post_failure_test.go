package reddit

import (
	"context"
	"errors"
	"github.com/originaleric/digcrawl/internal/browser"
	"testing"
)

func TestRSSFailureDoesNotReplaceBrowserFault(t *testing.T) {
	failure := &browser.BrowseError{Code: browser.ErrorBrowserTimeout, Stage: "acquire", Cause: context.DeadlineExceeded, Diagnostics: map[string]any{"operation": "send_queue", "diagnostic_id": "operation-1", "next_actions": []string{"wait"}}}
	err := postBrowserFailure(failure, errors.New("RSS unavailable"))
	if err != failure || failure.ToolErrorDetails()["next_actions"] == nil {
		t.Fatalf("browser fault hidden: %v", err)
	}
}
