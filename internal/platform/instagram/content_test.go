package instagram

import (
	"context"
	"testing"
	"time"
)

func TestWaitConfiguredRateLimitHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := waitConfiguredRateLimit(ctx, 5000)
	if err == nil {
		t.Fatal("expected canceled context error")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("rate limit wait did not return promptly after context cancellation")
	}
}
