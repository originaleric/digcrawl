package platformprov

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
	"github.com/originaleric/digcrawl/internal/testutil"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
)

func TestWechatChannelsUsesSharedPlatformReader(t *testing.T) {
	const share = "https://weixin.qq.com/sph/AauAggqePs"
	for _, tc := range []struct {
		name                       string
		collect, media, download   bool
		retain, screenshot, login  bool
		wantVideo, wantPublic      bool
		wantRetention, wantHandoff bool
		wantAttachment             bool
	}{
		{name: "public screenshot", screenshot: true, wantPublic: true},
		{name: "collect without download", collect: true, media: true, wantPublic: true},
		{name: "collect with media hidden", collect: true, download: true, wantPublic: true},
		{name: "collect downloads for shared exporter", collect: true, media: true, download: true, wantVideo: true, wantHandoff: true},
		{name: "direct MP4 and screenshot", media: true, screenshot: true, wantVideo: true, wantPublic: true, wantAttachment: true},
		{name: "profile retention", retain: true, wantVideo: true, wantRetention: true},
		{name: "collect profile retention", collect: true, media: true, download: true, retain: true, wantVideo: true, wantRetention: true},
		{name: "login required", collect: true, media: true, download: true, login: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := &config.Config{
				Collect:       config.CollectConfig{DownloadMedia: &tc.download, MaxMediaBytes: 1 << 20},
				VideoDownload: config.VideoDownloadConfig{Dir: filepath.Join(root, "retained"), MaxBytes: 2 << 20},
			}
			if tc.retain {
				cfg.VideoDownload.Providers = map[string]bool{"wechat_channels": true}
			}
			store, err := artifact.NewDiskStore(filepath.Join(root, "artifacts"), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "fixture.mp4")
			if err := os.WriteFile(source, []byte(testutil.MP4), 0o600); err != nil {
				t.Fatal(err)
			}

			originalResolve, originalDownload := resolveWechatShareVideo, downloadWechatShareVideo
			t.Cleanup(func() { resolveWechatShareVideo, downloadWechatShareVideo = originalResolve, originalDownload })
			var resolveCount, downloadCount, publicCount, cleanupCount int
			resolveWechatShareVideo = func(_ context.Context, in iplatform.ReadInput, active *config.Config) (*iplatform.Content, *wechatchannels.ShareVideo, error) {
				resolveCount++
				if in.URL != share || active != cfg {
					t.Fatal("video resolution lost the URL or active profile")
				}
				if tc.login {
					return nil, nil, iplatform.NewReadError(iplatform.ErrLoginRequired, "login required", "wechat_channels",
						map[string]any{"login_platform": "wechat_channels"})
				}
				return &iplatform.Content{Platform: "wechat_channels", ContentType: "video", SourceURL: in.URL, Text: "caption"},
					&wechatchannels.ShareVideo{}, nil
			}
			downloadWechatShareVideo = func(_ *wechatchannels.ShareVideo, ctx context.Context, maxBytes int64, retainDir string) (string, func(), error) {
				downloadCount++
				if _, ok := ctx.Deadline(); !ok || maxBytes <= 0 {
					t.Fatal("video download had no size or time limit")
				}
				if tc.retain && maxBytes != cfg.VideoDownload.MaxBytes || tc.collect && !tc.retain && maxBytes != cfg.Collect.MaxMediaBytes {
					t.Fatalf("unexpected video size limit %d", maxBytes)
				}
				if tc.retain {
					if retainDir != cfg.VideoDownload.Dir {
						t.Fatalf("unexpected retention directory %s", retainDir)
					}
					retainedPath := filepath.Join(retainDir, "video.mp4")
					if err := os.MkdirAll(retainDir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(retainedPath, []byte(testutil.MP4), 0o600); err != nil {
						t.Fatal(err)
					}
					return retainedPath, func() { cleanupCount++ }, nil
				}
				return source, func() { cleanupCount++ }, nil
			}
			read := func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
				publicCount++
				content := &iplatform.Content{Platform: "wechat_channels", ContentType: "video", SourceURL: in.URL, Text: "caption"}
				if in.IncludeScreenshot {
					content.ScreenshotBase64 = "iVBORw0KGgo="
				}
				return content, nil
			}

			ctx := t.Context()
			if tc.collect {
				ctx = executor.WithCollectExecution(ctx)
			}
			input, err := json.Marshal(platformReadInput{URL: share, IncludeMedia: tc.media, IncludeScreenshot: tc.screenshot})
			if err != nil {
				t.Fatal(err)
			}
			handler := platformReadHandlerNamedWithConfig(ToolWechatChannelsRead, wechatDefaultDomains, nil, store, read, cfg, resolveWechatChannelsVideo)
			out, artifacts, err := handler(ctx, &protocol.ToolCall{Tool: ToolWechatChannelsRead, Input: input})
			if tc.login {
				var readErr *iplatform.ReadError
				if !errors.As(err, &readErr) || readErr.Code != iplatform.ErrLoginRequired ||
					outputPlatformMetadata(out)["login_platform"] != "wechat_channels" ||
					resolveCount != 1 || downloadCount != 0 || publicCount != 0 {
					t.Fatalf("unexpected login result: output=%v err=%v", out, err)
				}
				return
			}
			if err != nil || out["text"] != "caption" {
				t.Fatalf("unexpected read result: output=%v err=%v", out, err)
			}
			if (downloadCount == 1) != tc.wantVideo || resolveCount != downloadCount || cleanupCount != downloadCount ||
				(publicCount == 1) != tc.wantPublic {
				t.Fatalf("unexpected calls: resolve=%d download=%d public=%d cleanup=%d",
					resolveCount, downloadCount, publicCount, cleanupCount)
			}
			meta := outputPlatformMetadata(out)
			if tc.wantRetention {
				rel, _ := meta["retained_media_relative_path"].(string)
				got, err := os.ReadFile(filepath.Join(cfg.VideoDownload.Dir, rel))
				if err != nil || string(got) != testutil.MP4 || meta["video_download_status"] != "saved" {
					t.Fatalf("retained video: metadata=%v err=%v", meta, err)
				}
			}
			video := outputPrimaryVideoMedia(out)
			if tc.wantHandoff || tc.wantAttachment {
				id, _ := video["artifact_id"].(string)
				got, mime, err := store.Get(ctx, id)
				if _, hasURL := video["url"]; hasURL || err != nil || mime != "video/mp4" || string(got) != testutil.MP4 {
					t.Fatalf("private MP4 handoff: media=%v mime=%q err=%v", video, mime, err)
				}
			} else if video != nil {
				t.Fatalf("unexpected video handoff: %v", video)
			}
			var hasVideo, hasScreenshot bool
			for _, art := range artifacts {
				hasVideo = hasVideo || art.Type == "video/mp4"
				hasScreenshot = hasScreenshot || art.Type == "image/png" && art.ID == out["screenshot_artifact_id"]
			}
			if hasVideo != tc.wantAttachment || hasScreenshot != tc.screenshot {
				t.Fatalf("unexpected response attachments: %+v", artifacts)
			}
		})
	}
}

func TestWechatChannelsCollectFallbackAfterRetentionFailure(t *testing.T) {
	const share = "https://weixin.qq.com/sph/AauAggqePs"
	downloadFailure := errors.New("download failed under Collect limits")
	for _, tc := range []struct {
		name         string
		media        bool
		fallback     bool
		noStore      bool
		cancelled    bool
		missingVideo bool
		fallbackErr  error
		wantErr      error
		wantHandoff  bool
	}{
		{name: "use Collect limits", media: true, fallback: true, wantHandoff: true},
		{name: "download failure", media: true, fallback: true, fallbackErr: downloadFailure, wantErr: downloadFailure},
		{name: "handoff failure", media: true, fallback: true, missingVideo: true, wantErr: os.ErrNotExist},
		{name: "caller cancelled", media: true, cancelled: true, wantErr: context.Canceled},
		{name: "retention only"},
		{name: "artifact store disabled", media: true, noStore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			blocked := filepath.Join(root, "blocked")
			if err := os.WriteFile(blocked, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			yes := true
			cfg := &config.Config{
				Collect: config.CollectConfig{DownloadMedia: &yes, MaxMediaBytes: 1 << 20, MediaTimeoutSec: 3},
				VideoDownload: config.VideoDownloadConfig{
					Dir: blocked, MaxBytes: 2 << 20, TimeoutSec: 30, Providers: map[string]bool{"wechat_channels": true},
				},
			}
			var store artifact.Store
			if !tc.noStore {
				disk, err := artifact.NewDiskStore(filepath.Join(root, "artifacts"), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				store = disk
			}
			ctx, cancel := context.WithCancel(executor.WithCollectExecution(t.Context()))
			defer cancel()
			retentionAttempts, collectAttempts, cleaned := 0, 0, 0
			resolve := func(_ context.Context, _ iplatform.ReadInput, _ *config.Config, _ platformReader) (*iplatform.Content, localVideoDownload, error) {
				content := &iplatform.Content{Platform: "wechat_channels", ContentType: "video", SourceURL: share, Title: "caption"}
				return content, func(ctx context.Context, maxBytes int64, retain bool, dir string) (string, func(), error) {
					if retain {
						retentionAttempts++
						if dir != blocked || maxBytes != cfg.VideoDownload.MaxBytes {
							t.Fatal("retention lost its own directory or size limit")
						}
						if tc.cancelled {
							cancel()
						}
						return (&wechatchannels.ShareVideo{}).Download(ctx, maxBytes, dir)
					}
					collectAttempts++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 3*time.Second || maxBytes != cfg.Collect.MaxMediaBytes || dir != "" || ctx.Err() != nil {
						t.Fatal("fallback lost its independent Collect budget or inherited a cancelled attempt")
					}
					if tc.fallbackErr != nil {
						return "", nil, tc.fallbackErr
					}
					path := filepath.Join(root, "fallback.mp4")
					if !tc.missingVideo {
						if err := os.WriteFile(path, []byte(testutil.MP4), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return path, func() { cleaned++ }, nil
				}, nil
			}
			input, err := json.Marshal(platformReadInput{URL: share, IncludeMedia: tc.media})
			if err != nil {
				t.Fatal(err)
			}
			handler := platformReadHandlerNamedWithConfig(ToolWechatChannelsRead, wechatDefaultDomains, nil, store, nil, cfg, resolve)
			out, _, err := handler(ctx, &protocol.ToolCall{Tool: ToolWechatChannelsRead, Input: input})
			if retentionAttempts != 1 || collectAttempts > 1 || (collectAttempts == 1) != tc.fallback {
				t.Fatalf("unexpected attempts: retention=%d Collect=%d", retentionAttempts, collectAttempts)
			}
			if (tc.wantHandoff || tc.missingVideo) && cleaned != 1 {
				t.Fatalf("temporary video cleanup ran %d times", cleaned)
			}
			var retentionErr *providerVideoDownloadError
			if tc.wantErr != nil || tc.noStore {
				if err == nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) || errors.As(err, &retentionErr) || out != nil {
					t.Fatalf("unavailable video must fail Collect: output=%v error=%v", out, err)
				}
				return
			}
			if !errors.As(err, &retentionErr) || outputPlatformMetadata(out)["video_download_status"] != "failed" {
				t.Fatalf("retention diagnostics lost: output=%v error=%v", out, err)
			}
			video := outputPrimaryVideoMedia(out)
			if !tc.wantHandoff {
				if video != nil {
					t.Fatalf("unexpected Collect video: %v", video)
				}
				return
			}
			id, _ := video["artifact_id"].(string)
			data, mime, err := store.Get(ctx, id)
			if err != nil || string(data) != testutil.MP4 || mime != "video/mp4" || video["url"] != nil {
				t.Fatalf("invalid Collect video handoff: media=%v mime=%q err=%v", video, mime, err)
			}
		})
	}
}

func TestWechatScreenshotAppliesConfiguredVideoAcquisition(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		frames, retain bool
	}{
		{name: "frame OCR", frames: true},
		{name: "retention", retain: true},
		{name: "frame OCR and retention", frames: true, retain: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			yes := true
			cfg := &config.Config{
				LocalBrowser: config.LocalBrowserConfig{CookieStoreDir: filepath.Join(root, "empty-session")},
			}
			if scenario.frames {
				cfg.Skill = config.SkillConfig{
					Media: config.SkillMediaConfig{DownloadMedia: true, Dir: root},
					OCR: config.OCRConfig{Enabled: &yes, Provider: "stub",
						VideoFrames: config.VideoFrameOCRConfig{Enabled: &yes}},
				}
			}
			if scenario.retain {
				cfg.VideoDownload.Providers = map[string]bool{"wechat_channels": true}
			}
			entry := WechatChannelsReadEntry(nil, nil, cfg)
			out, attachments, err := entry.Handler(t.Context(), &protocol.ToolCall{
				Tool:  ToolWechatChannelsRead,
				Input: []byte(`{"url":"https://weixin.qq.com/sph/AauAggqePs","include_screenshot":true}`),
			})
			var readErr *iplatform.ReadError
			if !errors.As(err, &readErr) || readErr.Code != iplatform.ErrLoginRequired ||
				outputPlatformMetadata(out)["login_platform"] != "wechat_channels" || len(attachments) != 0 {
				t.Fatalf("configured video acquisition must require its source login: out=%v attachments=%v err=%v", out, attachments, err)
			}
			if scenario.frames && (!cfg.Skill.OCR.EnabledOrDefault() || !cfg.Skill.OCR.VideoFrames.EnabledOrDefault()) ||
				scenario.retain && !cfg.VideoDownload.EnabledFor("wechat_channels") {
				t.Fatal("request changed the configured media policy")
			}
		})
	}
}
