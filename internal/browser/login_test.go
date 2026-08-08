package browser

import (
	"strings"
	"testing"
)

func TestThreadsLoginSpecUsesCurrentDomain(t *testing.T) {
	t.Parallel()

	spec, err := LoginSpecFor("threads")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IndexURL != "https://www.threads.com" || spec.CookieDomain != "www.threads.com" {
		t.Fatalf("unexpected threads login spec: %+v", spec)
	}
}

func TestWaitForLoginConfirmationRequiresNewline(t *testing.T) {
	if err := waitForLoginConfirmation(strings.NewReader("\n")); err != nil {
		t.Fatal(err)
	}
	if err := waitForLoginConfirmation(strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "未收到登录确认") {
		t.Fatalf("error=%v", err)
	}
}
