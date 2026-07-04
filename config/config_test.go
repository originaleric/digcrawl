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

func TestOCRConfigHelpers(t *testing.T) {
	t.Parallel()

	if (OCRConfig{}).EnabledOrDefault() {
		t.Fatal("zero OCRConfig should default to disabled")
	}
	enabled := true
	if !(OCRConfig{Enabled: &enabled}).EnabledOrDefault() {
		t.Fatal("explicit enabled OCR should be enabled")
	}
	if got := (OCRConfig{}).ProviderOrDefault(); got != "openai_compatible" {
		t.Fatalf("ProviderOrDefault=%q", got)
	}
	if got := (OCRConfig{Provider: "stub"}).ProviderOrDefault(); got != "stub" {
		t.Fatalf("ProviderOrDefault stub=%q", got)
	}
	if got := (OCRConfig{Provider: "zhipu_ocr"}).ProviderOrDefault(); got != "zhipu_ocr" {
		t.Fatalf("ProviderOrDefault zhipu_ocr=%q", got)
	}
	if got := (OCRConfig{Provider: "glm-ocr"}).ProviderOrDefault(); got != "zhipu_ocr" {
		t.Fatalf("ProviderOrDefault glm-ocr=%q", got)
	}
	if got := (OCRConfig{}).BaseURLOrDefault(); got != "https://api.openai.com/v1" {
		t.Fatalf("BaseURLOrDefault=%q", got)
	}
	if got := (OCRConfig{}).ModelOrDefault(); got != "gpt-4o-mini" {
		t.Fatalf("ModelOrDefault=%q", got)
	}
	if got := (OCRConfig{}).TimeoutSecOrDefault(); got != 30 {
		t.Fatalf("TimeoutSecOrDefault=%d", got)
	}
	if got := (OCRConfig{}).MaxImagesOrDefault(); got != 8 {
		t.Fatalf("MaxImagesOrDefault=%d", got)
	}
	if got := (OCRConfig{}).MaxImageBytesOrDefault(); got != 5*1024*1024 {
		t.Fatalf("MaxImageBytesOrDefault=%d", got)
	}
	if !(OCRConfig{}).AppendToMarkdownOrDefault() {
		t.Fatal("AppendToMarkdown should default to true")
	}
	if (OCRConfig{}).AppendToTextOrDefault() {
		t.Fatal("AppendToText should default to false")
	}
}
