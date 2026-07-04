package config

import "testing"

func TestCollectPersistModeHelpers(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false

	cases := []struct {
		mode       string
		indexEn    *bool
		wantIndex  bool
		wantSQLite bool
	}{
		{"", &enabled, true, false},
		{"files", &enabled, false, false},
		{"index", &enabled, true, false},
		{"sqlite", &enabled, false, true},
		{"both", &enabled, true, true},
		{"index", &disabled, false, false},
		{"both", &disabled, false, true},
	}
	for _, tc := range cases {
		c := CollectConfig{PersistMode: tc.mode, IndexEnabled: tc.indexEn}
		if c.PersistModeOrDefault() == "index" && tc.mode == "" {
			// ok
		}
		if c.UseIndexJSON() != tc.wantIndex {
			t.Fatalf("mode=%q indexEn=%v UseIndexJSON=%v want %v", tc.mode, tc.indexEn, c.UseIndexJSON(), tc.wantIndex)
		}
		if c.UseSQLite() != tc.wantSQLite {
			t.Fatalf("mode=%q UseSQLite=%v want %v", tc.mode, c.UseSQLite(), tc.wantSQLite)
		}
	}
	if got := (CollectConfig{}).DBPathOrDefault(); got != "storage/digcrawl/collect.db" {
		t.Fatalf("DBPathOrDefault=%q", got)
	}
}

func TestXAPIConfigHelpers(t *testing.T) {
	t.Parallel()

	if !(XAPIConfig{}).EnabledOrDefault() {
		t.Fatal("zero XAPIConfig should default to enabled")
	}
	if !(XAPIConfig{}).OEmbedEnabledOrDefault() {
		t.Fatal("zero XAPIConfig should default to oEmbed enabled")
	}
	disabled := false
	if (XAPIConfig{APIEnabled: &disabled}).EnabledOrDefault() {
		t.Fatal("explicit disabled API should be disabled")
	}
	if (XAPIConfig{OEmbedEnabled: &disabled}).OEmbedEnabledOrDefault() {
		t.Fatal("explicit disabled oEmbed should be disabled")
	}
	if got := (XAPIConfig{}).AuthModeOrDefault(); got != "app_only" {
		t.Fatalf("AuthModeOrDefault=%q", got)
	}
	if got := (XAPIConfig{BearerToken: "token"}).AuthModeOrDefault(); got != "bearer" {
		t.Fatalf("AuthModeOrDefault with bearer=%q", got)
	}
	if got := (XAPIConfig{}).APIBaseURLOrDefault(); got != "https://api.twitter.com" {
		t.Fatalf("APIBaseURLOrDefault=%q", got)
	}
	if got := (XAPIConfig{}).TokenURLOrDefault(); got != "https://api.twitter.com/oauth2/token" {
		t.Fatalf("TokenURLOrDefault=%q", got)
	}
	if got := (XAPIConfig{}).OEmbedURLOrDefault(); got != "https://publish.twitter.com/oembed" {
		t.Fatalf("OEmbedURLOrDefault=%q", got)
	}
	if got := (XAPIConfig{}).APITimeoutSecOrDefault(); got != 8 {
		t.Fatalf("APITimeoutSecOrDefault=%d", got)
	}
	if got := (XAPIConfig{}).APIMaxRetriesOrDefault(); got != 1 {
		t.Fatalf("APIMaxRetriesOrDefault=%d", got)
	}
}
