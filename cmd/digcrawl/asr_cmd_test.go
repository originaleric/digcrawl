package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestNormalizeASRTranscribeArgsAllowsFlagsAfterPath(t *testing.T) {
	args := normalizeASRTranscribeArgs([]string{
		"video.mp4",
		"--provider", "local",
		"--model=small",
		"--response-format", "text",
	})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := fs.String("provider", "", "")
	model := fs.String("model", "", "")
	responseFormat := fs.String("response-format", "", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *provider != "local" || *model != "small" || *responseFormat != "text" {
		t.Fatalf("provider=%q model=%q responseFormat=%q args=%+v", *provider, *model, *responseFormat, args)
	}
	if got := fs.Arg(0); got != "video.mp4" {
		t.Fatalf("positional=%q args=%+v", got, args)
	}
}

func TestNormalizeASRTranscribeArgsKeepsFlagsBeforePath(t *testing.T) {
	args := normalizeASRTranscribeArgs([]string{
		"--provider", "local",
		"--timeout", "1200",
		"--model", "small",
		"video.mp4",
	})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := fs.String("provider", "", "")
	timeout := fs.Int("timeout", 0, "")
	model := fs.String("model", "", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *provider != "local" || *timeout != 1200 || *model != "small" {
		t.Fatalf("provider=%q timeout=%d model=%q args=%+v", *provider, *timeout, *model, args)
	}
	if got := fs.Arg(0); got != "video.mp4" {
		t.Fatalf("positional=%q args=%+v", got, args)
	}
}

func TestLocalASRSetupConfigUsesLocalDefaultInsteadOfAPIModel(t *testing.T) {
	configured := config.ASRConfig{Provider: "api", Model: "whisper-1"}
	setup := localASRSetupConfig(configured, "")
	if setup.ProviderOrDefault() != "local" || setup.ModelOrDefault() != "small" {
		t.Fatalf("setup=%+v, want local/small", setup)
	}
	if localASRSetupMatchesConfiguredDefault(configured, setup) {
		t.Fatal("API default must not be reported as ready for local transcription")
	}
}

func TestLocalASRSetupConfigKeepsExplicitLocalModelAndHonorsOverride(t *testing.T) {
	configured := config.ASRConfig{Provider: "local", Model: "small"}
	if setup := localASRSetupConfig(configured, ""); !localASRSetupMatchesConfiguredDefault(configured, setup) {
		t.Fatalf("local default was not preserved: %+v", setup)
	}
	setup := localASRSetupConfig(configured, "base")
	if setup.ModelOrDefault() != "base" || localASRSetupMatchesConfiguredDefault(configured, setup) {
		t.Fatalf("override setup=%+v", setup)
	}
}

func TestPlanLocalASRSetupKeepsCustomRunnerOutsideDigCrawlOwnership(t *testing.T) {
	custom := config.ASRConfig{Provider: "local", LocalCommand: "custom-asr"}
	if _, err := planLocalASRSetup(custom, false); err == nil || !strings.Contains(err.Error(), "--skip-model") {
		t.Fatalf("custom runner without --skip-model error=%v", err)
	}
	plan, err := planLocalASRSetup(custom, true)
	if err != nil || plan.managesRuntime || plan.skipModel {
		t.Fatalf("custom runner plan=%+v err=%v", plan, err)
	}
	managed, err := planLocalASRSetup(config.ASRConfig{Provider: "local"}, true)
	if err != nil || !managed.managesRuntime || !managed.skipModel {
		t.Fatalf("default runner plan=%+v err=%v", managed, err)
	}
}

func TestConfirmASRSetupDescribesWhetherModelWillDownload(t *testing.T) {
	for _, test := range []struct {
		name      string
		skipModel bool
		want      string
		dontWant  string
	}{
		{name: "model", want: `model "base"`, dontWant: "will not download a model"},
		{name: "runtime only", skipModel: true, want: "will not download a model", dontWant: `model "base"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			confirmed, err := confirmASRSetup(strings.NewReader("y\n"), &output, "base", test.skipModel)
			if err != nil || !confirmed {
				t.Fatalf("confirmed=%t err=%v", confirmed, err)
			}
			if got := output.String(); !strings.Contains(got, test.want) || strings.Contains(got, test.dontWant) {
				t.Fatalf("prompt=%q", got)
			}
		})
	}
}

func TestLoadASRTargetUsesSkillProfileSettingsAndAnchoredCache(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	profilePath := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte("Skill:\n  ASR:\n    Provider: local\n    Model: base\n    ModelCacheDir: models\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := loadASRTarget("", "auto")
	if err != nil {
		t.Fatal(err)
	}
	if target.scope != config.ASRScopeSkill || target.settings.ProviderOrDefault() != "local" || target.settings.ModelOrDefault() != "base" {
		t.Fatalf("target=%+v", target)
	}
	if want := filepath.Join(filepath.Dir(profilePath), "models"); target.settings.ModelCacheDir != want {
		t.Fatalf("cache=%q want %q", target.settings.ModelCacheDir, want)
	}
}

func TestLoadASRTargetRejectsAmbiguousConfigWithoutScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "combined.yaml")
	if err := os.WriteFile(path, []byte("ASR:\n  Provider: local\nSkill:\n  ASR:\n    Provider: local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadASRTarget(path, "auto"); err == nil || !strings.Contains(err.Error(), "--scope root or --scope skill") {
		t.Fatalf("auto scope error=%v", err)
	}
	target, err := loadASRTarget(path, "skill")
	if err != nil || target.scope != config.ASRScopeSkill {
		t.Fatalf("skill target=%+v err=%v", target, err)
	}
}

func TestLoadASRTargetSkipsProjectConfigWithoutASRDomain(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	const skippedRuntimeEnv = "DIGCRAWL_ASR_SKIPPED_RUNTIME_ENV_TEST"
	t.Setenv(skippedRuntimeEnv, "")
	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/config.yaml", []byte("Gateway:\n  InstanceID: unrelated\nRuntimeEnv:\n  Enabled: true\n  Variables:\n    "+skippedRuntimeEnv+": should-not-apply\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profilePath := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte("Skill:\n  ASR:\n    Provider: local\n    Model: base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := loadASRTarget("", "auto")
	if err != nil || target.scope != config.ASRScopeSkill || target.settings.ModelOrDefault() != "base" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if got := os.Getenv(skippedRuntimeEnv); got != "" {
		t.Fatalf("skipped project RuntimeEnv was applied: %q", got)
	}
}

func TestLoadASRTargetRejectsUndeclaredScopeAndNoConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if _, err := loadASRTarget("", "auto"); err == nil || !strings.Contains(err.Error(), "digcrawl init --profile skill") {
		t.Fatalf("missing config error=%v", err)
	}
	profilePath := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte("Skill:\n  ASR:\n    Provider: local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadASRTarget(profilePath, "root"); err == nil || !strings.Contains(err.Error(), "no root ASR") {
		t.Fatalf("root scope error=%v", err)
	}
}
