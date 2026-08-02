package policy

import (
	"testing"
)

func TestValidateURLDomain(t *testing.T) {
	t.Parallel()
	if err := ValidateURLDomain("https://8.8.8.8/s/abc", []string{"8.8.8.8"}, nil); err != nil {
		t.Fatalf("expected allowed: %v", err)
	}
	if err := ValidateURLDomain("https://1.1.1.1/x", []string{"8.8.8.8"}, nil); err == nil {
		t.Fatal("expected domain error")
	}
	if err := ValidateURLDomain("http://127.0.0.1/", nil, nil); err != nil {
		t.Fatalf("an empty allowlist must not restrict a local target: %v", err)
	}
}

func TestValidateToolAllowed(t *testing.T) {
	t.Parallel()
	if err := ValidateToolAllowed("browser.browse", []string{"browser.browse"}); err != nil {
		t.Fatalf("expected allowed: %v", err)
	}
	if err := ValidateToolAllowed("file.read", []string{"browser.browse"}); err == nil {
		t.Fatal("expected tool not allowed")
	}
}
