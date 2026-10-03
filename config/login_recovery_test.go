package config

import "testing"

func TestLoginRecoveryDefaultsRequireOptIn(t *testing.T) {
	cfg := WatchlistLoginRecoveryConfig{}
	if cfg.EnabledOrDefault() {
		t.Fatal("login recovery must be off by default")
	}
	if cfg.LookbackOrDefault() != "24h" {
		t.Fatalf("unexpected default lookback: %q", cfg.LookbackOrDefault())
	}
}
