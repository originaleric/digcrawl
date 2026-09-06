package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	browserprov "github.com/originaleric/digcrawl/provider/browser"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/skill"
)

func TestExecuteUnknownTool(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := New(reg, Options{InstanceID: "test"})
	call := &protocol.ToolCall{
		Type: protocol.TypeToolCall,
		ID:   "call_1",
		Tool: "missing.tool",
	}
	res := rt.Execute(context.Background(), call)
	if res.Status != "error" || res.Error == nil {
		t.Fatalf("expected error result, got %+v", res)
	}
}

func TestRecommendedCallTimeoutIncludesExplicitASRBudgets(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		LocalBrowser: config.LocalBrowserConfig{TotalTimeoutSec: 90},
		Skill: config.SkillConfig{
			Media: config.SkillMediaConfig{TimeoutSec: 30},
			ASR:   config.ASRConfig{TimeoutSec: 300, MaxTimeoutSec: 600},
		},
		ASR: config.ASRConfig{MaxTimeoutSec: 900},
	}
	plain := &protocol.ToolCall{Input: json.RawMessage(`{"url":"https://example.com"}`)}
	if got := RecommendedCallTimeout(cfg, plain); got != 120*time.Second {
		t.Fatalf("plain timeout=%s want 2m", got)
	}
	asr := &protocol.ToolCall{Input: json.RawMessage(`{"include_asr":true}`)}
	if got := RecommendedCallTimeout(cfg, asr); got != 1380*time.Second {
		t.Fatalf("ASR timeout=%s want 23m", got)
	}
	summary := &protocol.ToolCall{Input: json.RawMessage(`{"include_summary":true}`)}
	if got := RecommendedCallTimeout(cfg, summary); got != 1440*time.Second {
		t.Fatalf("summary timeout=%s want 24m", got)
	}
	direct := &protocol.ToolCall{Tool: "asr.transcribe"}
	if got := RecommendedCallTimeout(cfg, direct); got != 930*time.Second {
		t.Fatalf("direct ASR timeout=%s want 15m30s", got)
	}
}

func TestRecommendedCallTimeoutUsesSkillMediaDefault(t *testing.T) {
	cfg := &config.Config{
		LocalBrowser: config.LocalBrowserConfig{TotalTimeoutSec: 90},
		Skill:        config.SkillConfig{ASR: config.ASRConfig{TimeoutSec: 300}},
	}
	call := &protocol.ToolCall{Input: json.RawMessage(`{"include_asr":true}`)}
	if got, want := RecommendedCallTimeout(cfg, call), 3960*time.Second; got != want {
		t.Fatalf("ASR timeout=%s want %s", got, want)
	}
}

func TestRecommendedCallTimeoutIncludesOptedInProviderRetention(t *testing.T) {
	cfg := &config.Config{
		LocalBrowser: config.LocalBrowserConfig{TotalTimeoutSec: 90},
		VideoDownload: config.VideoDownloadConfig{
			Providers:  map[string]bool{"toutiao": true},
			TimeoutSec: 300,
		},
	}
	// The handler asks its reader for media because of this opt-in after the
	// Runtime selects a deadline, so include_media need not be present here.
	call := &protocol.ToolCall{Tool: "toutiao.video.read", Input: json.RawMessage(`{"url":"https://example.com/video"}`)}
	if got, want := RecommendedCallTimeout(cfg, call), 420*time.Second; got != want {
		t.Fatalf("retention timeout=%s want %s", got, want)
	}
	notOptedIn := &protocol.ToolCall{Tool: "douyin.video.read", Input: call.Input}
	if got, want := RecommendedCallTimeout(cfg, notOptedIn), 120*time.Second; got != want {
		t.Fatalf("non-opted provider timeout=%s want %s", got, want)
	}
}

func TestRecommendedCallTimeoutReadsActiveSkillSnapshotDuringUpdates(t *testing.T) {
	old := config.Skill()
	t.Cleanup(func() { config.SetSkill(old) })
	enabled := true
	first := config.SkillConfig{Media: config.SkillMediaConfig{TimeoutSec: 10, MaxASRCandidates: 1}, ASR: config.ASRConfig{Enabled: &enabled, MaxTimeoutSec: 20}}
	second := config.SkillConfig{Media: config.SkillMediaConfig{TimeoutSec: 30, MaxASRCandidates: 2}, ASR: config.ASRConfig{Enabled: &enabled, MaxTimeoutSec: 40}}
	config.SetSkill(first)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			config.SetSkill(second)
			config.SetSkill(first)
		}
	}()
	call := &protocol.ToolCall{Input: json.RawMessage(`{"include_asr":true}`)}
	for i := 0; i < 1000; i++ {
		if got := RecommendedCallTimeout(config.Get(), call); got <= 0 {
			t.Fatalf("timeout=%s", got)
		}
	}
	<-done
}

func TestRecommendedCallTimeoutRespectsASRCandidateLimit(t *testing.T) {
	cfg := &config.Config{Skill: config.SkillConfig{
		Media: config.SkillMediaConfig{TimeoutSec: 30, MaxASRCandidates: 1},
		ASR:   config.ASRConfig{TimeoutSec: 300, MaxTimeoutSec: 600},
	}}
	call := &protocol.ToolCall{Input: json.RawMessage(`{"include_asr":true}`)}
	if got, want := RecommendedCallTimeout(cfg, call), 750*time.Second; got != want {
		t.Fatalf("ASR timeout=%s want %s", got, want)
	}
}

func TestRecommendedCallTimeoutIncludesExplicitOCRBudgets(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		LocalBrowser: config.LocalBrowserConfig{TotalTimeoutSec: 90},
		Skill: config.SkillConfig{
			Media: config.SkillMediaConfig{TimeoutSec: 30},
			OCR: config.OCRConfig{
				Enabled:     &enabled,
				TimeoutSec:  20,
				MaxImages:   3,
				VideoFrames: config.VideoFrameOCRConfig{Enabled: &enabled, TimeoutSec: 40, MaxTotalFrames: 2},
			},
		},
	}
	call := &protocol.ToolCall{Input: json.RawMessage(`{"include_ocr":true}`)}
	if got, want := RecommendedCallTimeout(cfg, call), 290*time.Second; got != want {
		t.Fatalf("OCR timeout=%s want %s", got, want)
	}
}

func TestExecutionTimeoutUsesConfiguredDefaultUnlessHostOverrides(t *testing.T) {
	t.Parallel()
	rt := New(registry.New(), Options{TimeoutForCall: func(*protocol.ToolCall) time.Duration { return 5 * time.Minute }})
	if got := rt.ExecutionTimeout(&protocol.ToolCall{}); got != 5*time.Minute {
		t.Fatalf("configured timeout=%s", got)
	}
	if got := rt.ExecutionTimeout(&protocol.ToolCall{Policy: protocol.CallPolicy{TimeoutMs: 2500}}); got != 2500*time.Millisecond {
		t.Fatalf("host timeout=%s", got)
	}
}

func TestManifest(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "browser.browse", Description: "test"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{"ok": true}, nil, nil
		},
	})
	rt := New(reg, Options{InstanceID: "inst_test"})
	m := rt.Manifest()
	if m.InstanceID != "inst_test" || len(m.Tools) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestManifestFiltersToolsNotAllowedByRuntime(t *testing.T) {
	reg := registry.New()
	for _, name := range []string{"browser.browse", "browser.action"} {
		reg.Register(registry.Entry{Descriptor: protocol.ToolDescriptor{Name: name}})
	}
	rt := New(reg, Options{AllowedTools: []string{"browser.browse"}})
	m := rt.Manifest()
	if len(m.Tools) != 1 || m.Tools[0].Name != "browser.browse" {
		t.Fatalf("manifest advertised tools outside the allowlist: %+v", m.Tools)
	}
}

func TestDefaultProvidersUseGatewayDomainsAsAnOptionalRestriction(t *testing.T) {
	base := *config.Get()
	for _, tc := range []struct {
		name string
		want []string
	}{
		{name: "open public web", want: nil},
		{name: "restricted web", want: []string{"example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Gateway.AllowedDomains = tc.want
			providers := DefaultProviders(&cfg, RegistryOptions{})
			var browserDomains, platformDomains []string
			for _, p := range providers {
				switch p := p.(type) {
				case *browserprov.Provider:
					browserDomains = p.ConfigDomains
				case *platformprov.Provider:
					platformDomains = p.ConfigDomains
				}
			}
			if !reflect.DeepEqual(browserDomains, tc.want) {
				t.Fatalf("browser domains=%v want=%v", browserDomains, tc.want)
			}
			if !reflect.DeepEqual(platformDomains, tc.want) {
				t.Fatalf("platform domains=%v want=%v", platformDomains, tc.want)
			}
		})
	}
}

func TestNewRuntimeFailsWhenEnabledArtifactStoreCannotStart(t *testing.T) {
	cfg := *config.Get()
	enabled := true
	cfg.Gateway.ArtifactEnabled = &enabled
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Gateway.ArtifactDir = path
	if _, err := NewRuntime(&cfg); err == nil {
		t.Fatal("enabled artifact store with an invalid directory should fail startup")
	}
}

func TestNewCollectorRuntimeHonorsArtifactEnabled(t *testing.T) {
	cfg := *config.Get()
	disabled := false
	cfg.Gateway.ArtifactEnabled = &disabled
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Gateway.ArtifactDir = path
	rt, err := NewCollectorRuntime(&cfg)
	if err != nil {
		t.Fatalf("disabled artifact store should not block collector startup: %v", err)
	}
	if rt.ArtifactStore() != nil {
		t.Fatal("collector should not create an artifact store when disabled")
	}
}

func TestValidateCallRequiresID(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := New(reg, Options{})
	res := rt.Execute(context.Background(), &protocol.ToolCall{Tool: "browser.browse"})
	if res.Status != "error" {
		t.Fatalf("expected error, got %+v", res)
	}
	_, _ = json.Marshal(res)
}

func TestExecuteNilCallReturnsError(t *testing.T) {
	t.Parallel()
	rt := New(registry.New(), Options{})
	res := rt.Execute(context.Background(), nil)
	if res.Status != "error" || res.Error == nil || res.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %+v", res)
	}
}

func TestExecuteRequiresApproval(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "danger.tool", RequiresUserApproval: true},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{"ok": true}, nil, nil
		},
	})
	rt := New(reg, Options{RequireApproval: true})
	res := rt.Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c1", Tool: "danger.tool"})
	if res.Status != "error" || res.Error == nil || res.Error.Code != "USER_APPROVAL_REQUIRED" {
		t.Fatalf("expected approval error, got %+v", res)
	}
	res = rt.Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c2", Tool: "danger.tool", Approved: true})
	if res.Status != "success" {
		t.Fatalf("expected success after approval, got %+v", res)
	}
}

func TestExecutePreservesStructuredOutputOnHandlerError(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "structured.error"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{"platform_metadata": map[string]any{"error_code": "LOGIN_REQUIRED"}}, []protocol.Artifact{{ID: "partial-media", Type: "video/mp4"}}, testStructuredError{code: "LOGIN_REQUIRED", msg: "login required"}
		},
	})
	rt := New(reg, Options{})
	res := rt.Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c1", Tool: "structured.error"})
	if res.Status != "error" || res.Error == nil || res.Error.Code != "LOGIN_REQUIRED" {
		t.Fatalf("expected structured error result, got %+v", res)
	}
	if len(res.Output) == 0 {
		t.Fatalf("expected structured output to be preserved")
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].ID != "partial-media" {
		t.Fatalf("expected partial output artifact to be preserved: %+v", res.Artifacts)
	}
}

func TestExecuteExposesStructuredErrorDetails(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "detailed.error"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return nil, nil, testDetailedError{}
		},
	})
	res := New(reg, Options{}).Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c1", Tool: "detailed.error"})
	if res.Error == nil || res.Error.Code != "BROWSER_PAGE_CLOSED" || res.Error.Details["stage"] != "extract_content" {
		t.Fatalf("expected detailed browser error, got %+v", res.Error)
	}
}

func TestExecutePreservesTypedMediaDownloadFailureFromSkill(t *testing.T) {
	enabled := true
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "media.failure"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			out := map[string]any{
				"content_type": "video",
				"media":        []any{map[string]any{"type": "video", "url": "http://127.0.0.1/video.mp4"}},
			}
			err := skill.Enhance(ctx, out, config.SkillConfig{
				Media: config.SkillMediaConfig{DownloadMedia: true, Dir: t.TempDir()},
				ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
			}, skill.EnhanceOptions{IncludeASR: true})
			return out, nil, err
		},
	})
	res := New(reg, Options{}).Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "media-download-failure", Tool: "media.failure"})
	if res.Status != "error" || res.Error == nil || res.Error.Code != "MEDIA_DOWNLOAD_FAILED" {
		t.Fatalf("result=%+v", res)
	}
	if res.Error.Details["component"] != "media_download" || res.Error.Details["reason"] != "invalid_media_url" || res.Error.Details["retryable"] != false {
		t.Fatalf("details=%+v", res.Error.Details)
	}
}

type testStructuredError struct {
	code string
	msg  string
}

func (e testStructuredError) Error() string            { return e.msg }
func (e testStructuredError) ToolErrorCode() string    { return e.code }
func (e testStructuredError) ToolErrorMessage() string { return e.msg }

type testDetailedError struct{}

func (testDetailedError) Error() string            { return "page closed" }
func (testDetailedError) ToolErrorCode() string    { return "BROWSER_PAGE_CLOSED" }
func (testDetailedError) ToolErrorMessage() string { return "page closed" }
func (testDetailedError) ToolErrorDetails() map[string]any {
	return map[string]any{"component": "local_browser", "stage": "extract_content", "retryable": true}
}

func TestDefaultRegistryIncludesASRTranscribe(t *testing.T) {
	t.Parallel()
	cfg := config.Get()
	reg, err := NewRegistry(cfg, RegistryOptions{AllowedReadPaths: []string{t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("asr.transcribe"); !ok {
		t.Fatalf("asr.transcribe not registered")
	}
}
