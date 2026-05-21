package policy

import "testing"

func TestValidateURLDomain(t *testing.T) {
	t.Parallel()
	if err := ValidateURLDomain("https://mp.weixin.qq.com/s/abc", []string{"mp.weixin.qq.com"}, nil); err != nil {
		t.Fatalf("expected allowed: %v", err)
	}
	if err := ValidateURLDomain("https://evil.example/x", []string{"mp.weixin.qq.com"}, nil); err == nil {
		t.Fatal("expected domain error")
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
