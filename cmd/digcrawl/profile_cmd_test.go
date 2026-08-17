package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestWriteAndValidateSkillProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "digcrawl-skill.yaml")
	if err := writeSkillProfile(path, false); err != nil {
		t.Fatalf("writeSkillProfile: %v", err)
	}
	cfg, err := validateProfile(path)
	if err != nil {
		t.Fatalf("validateProfile: %v", err)
	}
	if !filepath.IsAbs(cfg.Gateway.ArtifactDir) || !filepath.IsAbs(cfg.LocalBrowser.CookieStoreDir) {
		t.Fatalf("profile paths must be absolute: artifact=%q cookie=%q", cfg.Gateway.ArtifactDir, cfg.LocalBrowser.CookieStoreDir)
	}
	if !filepath.IsAbs(cfg.Skill.ASR.ModelCacheDir) {
		t.Fatalf("Skill.ASR.ModelCacheDir must be absolute: %q", cfg.Skill.ASR.ModelCacheDir)
	}
	if cfg.Skill.Media.DownloadMedia || cfg.Skill.ASR.EnabledOrDefault() {
		t.Fatal("new Skill profile must not opt into local media or ASR")
	}
	if !containsTool(cfg.Gateway.AllowedTools, "skill.asr.enable") {
		t.Fatal("new Skill profile must expose the confirmation-protected ASR enable tool")
	}
	for _, prohibited := range []string{"browser.action", "browser.snapshot", "asr.transcribe"} {
		for _, allowed := range cfg.Gateway.AllowedTools {
			if allowed == prohibited {
				t.Fatalf("new Skill profile exposed %q", prohibited)
			}
		}
	}
	if err := writeSkillProfile(path, false); err == nil {
		t.Fatal("existing profile should require --force")
	}
}

func TestAllowSkillASRConfigurationToolOnlyForDefaultProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defaultCfg := &config.Config{SourcePath: config.DefaultSkillProfilePath()}
	allowSkillASRConfigurationTool(defaultCfg)
	if !containsTool(defaultCfg.Gateway.AllowedTools, "skill.asr.enable") {
		t.Fatal("default Skill profile should expose the ASR enable tool in memory")
	}

	customCfg := &config.Config{SourcePath: filepath.Join(t.TempDir(), "custom.yaml")}
	allowSkillASRConfigurationTool(customCfg)
	if containsTool(customCfg.Gateway.AllowedTools, "skill.asr.enable") {
		t.Fatal("custom profile must not receive the standard Skill configuration tool")
	}
}

func containsTool(tools []string, want string) bool {
	for _, tool := range tools {
		if tool == want {
			return true
		}
	}
	return false
}

func TestValidateProfileRejectsRelativeRequiredPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, []byte("Gateway:\n  ArtifactDir: artifacts\nLocalBrowser:\n  CookieStoreDir: cookies\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := validateProfile(path)
	if err == nil || !strings.Contains(err.Error(), "Gateway.ArtifactDir") {
		t.Fatalf("validateProfile error=%v", err)
	}
}

func TestProfilePathForMCPUsesUserSkillProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got, want := profilePathForMCP(""), config.DefaultSkillProfilePath(); got != want {
		t.Fatalf("profilePathForMCP()=%q want %q", got, want)
	}
	if got := profilePathForMCP("/tmp/explicit.yaml"); got != "/tmp/explicit.yaml" {
		t.Fatalf("explicit profile path=%q", got)
	}
}

func TestValidateProfileRejectsIncompleteASRConfig(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "api key missing",
			yaml: "Skill:\n  Media:\n    DownloadMedia: true\n  ASR:\n    Enabled: true\n    Provider: api\n",
			want: "Skill.ASR.APIKey",
		},
		{
			name: "unknown provider",
			yaml: "Skill:\n  Media:\n    DownloadMedia: true\n  ASR:\n    Enabled: true\n    Provider: unsupported\n",
			want: "Skill.ASR.Provider",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "profile.yaml")
			data := "Gateway:\n  ArtifactDir: " + dir + "\nLocalBrowser:\n  CookieStoreDir: " + dir + "\n" + strings.Replace(tt.yaml, "    DownloadMedia: true", "    DownloadMedia: true\n    Dir: "+dir, 1)
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := validateProfile(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateProfileExpandsASRAPIKeyEnvironment(t *testing.T) {
	t.Setenv("DIGCRAWL_TEST_ASR_API_KEY", "test-key")
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	data := []byte("Gateway:\n  ArtifactDir: " + dir + "\nLocalBrowser:\n  CookieStoreDir: " + dir + "\nSkill:\n  Media:\n    DownloadMedia: true\n    Dir: " + dir + "\n  ASR:\n    Enabled: true\n    Provider: api\n    APIKey: ${DIGCRAWL_TEST_ASR_API_KEY}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateProfile(path); err != nil {
		t.Fatal(err)
	}
}

func TestCheckASRProviderRejectsUnavailableLocalCommand(t *testing.T) {
	enabled := true
	err := checkASRProvider(&config.Config{Skill: config.SkillConfig{ASR: config.ASRConfig{
		Enabled: &enabled, Provider: "local", LocalCommand: "digcrawl-test-command-that-does-not-exist",
	}}})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error=%v", err)
	}
}
