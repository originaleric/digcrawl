package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	internalvideo "github.com/originaleric/digcrawl/internal/video"
)

func TestNormalizeVideoArgsAllowsFlagsAfterSource(t *testing.T) {
	args := normalizeVideoArgs([]string{"meeting.mp4", "--mode", "both", "--format=md"})
	if got := strings.Join(args, " "); got != "--mode both --format=md meeting.mp4" {
		t.Fatalf("args=%q", got)
	}
}

func TestValidateVideoCLIOptions(t *testing.T) {
	if err := validateVideoCLIOptions(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := validateVideoCLIOptions(0, 1); err == nil || !strings.Contains(err.Error(), "--max-bytes") {
		t.Fatalf("max bytes error=%v", err)
	}
	if err := validateVideoCLIOptions(1, 0); err == nil || !strings.Contains(err.Error(), "--download-timeout") {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestFormatVideoResult(t *testing.T) {
	result := &internalvideo.Result{
		Source:   internalvideo.Source{Input: "meeting.mp4", DurationSeconds: 12.5, Bytes: 10},
		Text:     "## ASR\n\nhello",
		Warnings: []string{"OCR: unavailable"},
	}
	jsonData, err := formatVideoResult(result, "json")
	if err != nil || !json.Valid(jsonData) {
		t.Fatalf("json=%q err=%v", jsonData, err)
	}
	mdData, err := formatVideoResult(result, "md")
	if err != nil || !strings.Contains(string(mdData), "# Video analysis") || !strings.Contains(string(mdData), "Warnings") {
		t.Fatalf("markdown=%q err=%v", mdData, err)
	}
	textData, err := formatVideoResult(result, "text")
	if err != nil || string(textData) != result.Text {
		t.Fatalf("text=%q err=%v", textData, err)
	}
	if _, err := formatVideoResult(result, "xml"); err == nil {
		t.Fatal("unsupported format succeeded")
	}
}

func TestVideoConfigPathPrefersProjectThenUserSkillProfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	skillPath := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("Skill: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := videoConfigPath("")
	if err != nil || path != skillPath {
		t.Fatalf("user Skill profile path = %q, %v; want %q", path, err, skillPath)
	}

	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/config.yaml", []byte("ASR: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err = videoConfigPath("")
	if err != nil || path != "config/config.yaml" {
		t.Fatalf("project config path = %q, %v", path, err)
	}

	path, err = videoConfigPath(" custom.yaml ")
	if err != nil || path != "custom.yaml" {
		t.Fatalf("explicit config path = %q, %v", path, err)
	}
}

func TestLoadVideoConfigFillsInactiveProjectProcessorsFromUserSkill(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/config.yaml", []byte("ASR:\n  Enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillPath := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("Skill:\n  ASR:\n    Enabled: true\n    Provider: stub\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadVideoConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ASR.EnabledOrDefault() || cfg.ASR.Provider != "stub" {
		t.Fatalf("effective ASR = %+v; want active user Skill ASR", cfg.ASR)
	}
}

func TestVideoAnalysisConfigUsesActiveSkillProcessors(t *testing.T) {
	enabled, disabled := true, false
	skillASR := config.ASRConfig{Enabled: &enabled, Provider: "skill-asr"}
	skillOCR := config.OCRConfig{
		Enabled:  &enabled,
		Provider: "skill-ocr",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled,
		},
	}
	cfg := &config.Config{
		ASR:   config.ASRConfig{Enabled: &disabled, Provider: "root-asr"},
		OCR:   config.OCRConfig{Enabled: &disabled, Provider: "root-ocr"},
		Skill: config.SkillConfig{ASR: skillASR, OCR: skillOCR},
	}

	effective := videoAnalysisConfig(cfg)
	if effective.ASR.Provider != "skill-asr" || !effective.ASR.EnabledOrDefault() {
		t.Fatalf("effective ASR = %+v; want active Skill ASR", effective.ASR)
	}
	if effective.OCR.Provider != "skill-ocr" || !videoOCREnabled(effective.OCR) {
		t.Fatalf("effective OCR = %+v; want active Skill OCR", effective.OCR)
	}
	if cfg.ASR.Provider != "root-asr" || cfg.OCR.Provider != "root-ocr" {
		t.Fatal("video config adaptation must not mutate the loaded configuration")
	}
}

func TestVideoAnalysisConfigPrefersActiveRootProcessors(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		ASR: config.ASRConfig{Enabled: &enabled, Provider: "root-asr"},
		OCR: config.OCRConfig{
			Enabled:  &enabled,
			Provider: "root-ocr",
			VideoFrames: config.VideoFrameOCRConfig{
				Enabled: &enabled,
			},
		},
		Skill: config.SkillConfig{
			ASR: config.ASRConfig{Enabled: &enabled, Provider: "skill-asr"},
			OCR: config.OCRConfig{
				Enabled:  &enabled,
				Provider: "skill-ocr",
				VideoFrames: config.VideoFrameOCRConfig{
					Enabled: &enabled,
				},
			},
		},
	}

	effective := videoAnalysisConfig(cfg)
	if effective.ASR.Provider != "root-asr" || effective.OCR.Provider != "root-ocr" {
		t.Fatalf("root processors must retain priority: ASR=%q OCR=%q", effective.ASR.Provider, effective.OCR.Provider)
	}
}
