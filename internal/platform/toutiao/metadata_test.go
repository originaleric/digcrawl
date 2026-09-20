package toutiao

import (
	"strings"
	"testing"
)

func TestMetadataScriptReturnsRodEvalCallback(t *testing.T) {
	script := MetadataScript()
	if !strings.HasPrefix(script, "() => {") {
		t.Fatalf("metadata script must be a Rod Eval callback: %q", script[:min(len(script), 80)])
	}
	if !strings.HasSuffix(script, "})(); }") {
		t.Fatalf("metadata callback must close and invoke its inner callback: %q", script[len(script)-min(len(script), 80):])
	}
}
