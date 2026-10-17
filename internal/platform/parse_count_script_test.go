package platform

import (
	"strings"
	"testing"
)

func TestWithParseCountJSReturnsAnEvalCallback(t *testing.T) {
	script := WithParseCountJS(`() => ({ count: parseCount("1.2万") })`)
	if !strings.HasPrefix(script, "() => {") {
		t.Fatalf("metadata script must begin with an Eval callback: %q", script[:min(len(script), 80)])
	}
	if !strings.Contains(script, "return (() => ({ count: parseCount") || !strings.HasSuffix(script, ")(); }") {
		t.Fatalf("metadata script must invoke the supplied callback: %q", script)
	}
}
