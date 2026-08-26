package executor

import (
	"context"
	"testing"
)

func TestCollectExecutionContextCannotBeSelectedByToolInput(t *testing.T) {
	if IsCollectExecution(context.Background()) {
		t.Fatal("ordinary execution context must not be Collect")
	}
	if !IsCollectExecution(WithCollectExecution(context.Background())) {
		t.Fatal("trusted Collect context was not retained")
	}
}
