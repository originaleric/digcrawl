package browser

import (
	"testing"

	"github.com/originaleric/digcrawl/protocol"
)

func TestValidateCallURLOpensPublicWebWhenDomainRestrictionIsUnset(t *testing.T) {
	if err := validateCallURL("https://8.8.8.8/article", &protocol.ToolCall{}, nil); err != nil {
		t.Fatalf("expected public URL to be allowed without a domain restriction: %v", err)
	}
}

func TestValidateCallURLAllowsLocalTargetsAndHonorsOptionalRestriction(t *testing.T) {
	if err := validateCallURL("http://127.0.0.1/admin", &protocol.ToolCall{}, nil); err != nil {
		t.Fatalf("expected an unrestricted local target to be allowed: %v", err)
	}
	if err := validateCallURL("https://1.1.1.1/article", &protocol.ToolCall{}, []string{"8.8.8.8"}); err == nil {
		t.Fatal("expected configured domain restriction to be enforced")
	}
}
