package asrprov

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/testutil"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/registry"
)

func TestEnableSkillASRUpdatesOnlyDefaultUserProfileAndActiveConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg := writeReadyExecutable(t, filepath.Dir(path), "ffmpeg")
	data := []byte("# preserve this comment\nUnrelated: keep\nSkill:\n  Media:\n    Dir: " + filepath.Join(filepath.Dir(path), "media") + "\n    DownloadMedia: false\n  ASR:\n    Enabled: false\n    Provider: stub\n    FFmpegPath: " + ffmpeg + "\n    APIKey: unchanged-test-value\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}

	out, _, err := EnableSkillASREntry().Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("enable Skill ASR: %v", err)
	}
	if out["media_download_enabled"] != true || out["asr_enabled"] != true {
		t.Fatalf("unexpected output: %#v", out)
	}
	if !config.Skill().Media.DownloadMedia || !config.Skill().ASR.EnabledOrDefault() {
		t.Fatal("current process configuration was not updated")
	}
	persisted, err := config.Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.Skill.Media.DownloadMedia || !persisted.Skill.ASR.EnabledOrDefault() {
		t.Fatal("standard user profile was not updated")
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# preserve this comment", "Unrelated: keep", "APIKey: unchanged-test-value"} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("profile rewrite lost unrelated content %q:\n%s", want, updated)
		}
	}
}

func TestEnableSkillASRRejectsCustomProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := config.Save(path, &config.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnableSkillASREntry().Handler(context.Background(), nil); err == nil {
		t.Fatal("custom MCP profile must not be modified")
	}
	if _, _, err := ConfigureSkillMediaEntry().Handler(context.Background(), &protocol.ToolCall{Input: []byte(`{"retain_downloaded_video":true}`)}); err == nil {
		t.Fatal("custom MCP profile must not receive media configuration")
	}
}

func TestConfigureSkillMediaUpdatesOnlyDefaultUserProfileAndActiveConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("# preserve this comment\nUnrelated: keep\nSkill:\n  Media:\n    DownloadMedia: false\n    Dir: " + filepath.Join(filepath.Dir(path), "media") + "\n    RetainDownloadedVideo: false\n    MaxBytes: 134217728\n  ASR:\n    Enabled: false\n    APIKey: unchanged-test-value\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{"retain_downloaded_video": true, "max_bytes": 268435456})
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := ConfigureSkillMediaEntry().Handler(context.Background(), &protocol.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("configure Skill media: %v", err)
	}
	if out["retain_downloaded_video"] != true || out["max_bytes"] != int64(268435456) {
		t.Fatalf("unexpected output: %#v", out)
	}
	if config.Skill().Media.RetainDownloadedVideo != true || config.Skill().Media.MaxBytes != 268435456 {
		t.Fatal("current process media configuration was not updated")
	}
	persisted, err := config.Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.Skill.Media.RetainDownloadedVideo || persisted.Skill.Media.MaxBytes != 268435456 {
		t.Fatalf("standard user profile was not updated: %+v", persisted.Skill.Media)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# preserve this comment", "Unrelated: keep", "APIKey: unchanged-test-value"} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("profile rewrite lost unrelated content %q:\n%s", want, updated)
		}
	}
}

func TestConfigureSkillMediaRejectsInvalidInputWithoutWriting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("Skill:\n  Media:\n    RetainDownloadedVideo: false\n    MaxBytes: 128\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{}`, `{"max_bytes":0}`} {
		if _, _, err := ConfigureSkillMediaEntry().Handler(context.Background(), &protocol.ToolCall{Input: []byte(input)}); err == nil {
			t.Fatalf("input %s unexpectedly succeeded", input)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("invalid media setting rewrote profile:\n%s", got)
	}
}

func TestConfigureSkillMediaAddsPortableDefaultDirectoryForLegacyProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Skill:\n  Media:\n    RetainDownloadedVideo: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"retain_downloaded_video":true}`)
	out, _, err := ConfigureSkillMediaEntry().Handler(context.Background(), &protocol.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("configure legacy Skill media: %v", err)
	}
	wantDir := filepath.Join(filepath.Dir(path), "media")
	if out["media_dir"] != wantDir {
		t.Fatalf("media directory=%q want %q", out["media_dir"], wantDir)
	}
	persisted, err := config.Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Skill.Media.Dir != wantDir {
		t.Fatalf("persisted media directory=%q want %q", persisted.Skill.Media.Dir, wantDir)
	}
}

func TestEnableSkillASRAndConcurrentSkillReadsUseStableSnapshots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg := writeReadyExecutable(t, filepath.Dir(path), "ffmpeg")
	if err := os.WriteFile(path, []byte("Skill:\n  Media:\n    Dir: "+filepath.Join(filepath.Dir(path), "media")+"\n    DownloadMedia: false\n  ASR:\n    Enabled: false\n    Provider: stub\n    FFmpegPath: "+ffmpeg+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	readComplete := make(chan struct{})
	go func() {
		defer close(readComplete)
		for {
			select {
			case <-done:
				return
			default:
				_ = config.Skill().ASR.EnabledOrDefault()
			}
		}
	}()
	if _, _, err := EnableSkillASREntry().Handler(context.Background(), nil); err != nil {
		close(done)
		<-readComplete
		t.Fatal(err)
	}
	close(done)
	<-readComplete
}

func TestEnableSkillASRRejectsUnavailableConfiguredDependenciesWithoutWriting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg := writeReadyExecutable(t, filepath.Dir(path), "ffmpeg")
	data := []byte("Skill:\n  Media:\n    Dir: " + filepath.Join(filepath.Dir(path), "media") + "\n    DownloadMedia: false\n  ASR:\n    Enabled: false\n    Provider: local\n    FFmpegPath: " + ffmpeg + "\n    LocalCommand: digcrawl-missing-local-asr-command\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnableSkillASREntry().Handler(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "configured ASR is not ready") {
		t.Fatalf("error=%v", err)
	}
	if config.Skill().Media.DownloadMedia || config.Skill().ASR.EnabledOrDefault() {
		t.Fatal("failed readiness check must not update active Skill configuration")
	}
	persisted, err := config.Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Skill.Media.DownloadMedia || persisted.Skill.ASR.EnabledOrDefault() {
		t.Fatal("failed readiness check must not persist the opt-in")
	}
}

func writeReadyExecutable(t *testing.T, dir, _ string) string {
	t.Helper()
	return testutil.FakeFFmpeg(t, dir)
}

func TestProviderRegistersSkillEnableToolOnlyForDefaultProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	reg := registry.New()
	if err := (&Provider{SkillProfilePath: config.DefaultSkillProfilePath()}).Register(context.Background(), reg); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get(ToolEnableSkillASR); !ok {
		t.Fatal("default Skill profile should register the configuration tool")
	}
	if _, ok := reg.Get(ToolConfigureSkillMedia); !ok {
		t.Fatal("default Skill profile should register the media configuration tool")
	}

	custom := registry.New()
	if err := (&Provider{SkillProfilePath: filepath.Join(t.TempDir(), "custom.yaml")}).Register(context.Background(), custom); err != nil {
		t.Fatal(err)
	}
	if _, ok := custom.Get(ToolEnableSkillASR); ok {
		t.Fatal("custom profile must not register the configuration tool")
	}
	if _, ok := custom.Get(ToolConfigureSkillMedia); ok {
		t.Fatal("custom profile must not register the media configuration tool")
	}
}
