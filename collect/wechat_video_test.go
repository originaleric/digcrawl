package collect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/testutil"
	"github.com/originaleric/digcrawl/protocol"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const wechatVideoTestURL = "https://weixin.qq.com/sph/AauAggqePs"

func TestCollectWechatChannelsRequiresLoginBeforeExport(t *testing.T) {
	root := t.TempDir()
	yes, no := true, false
	cfg := &config.Config{
		SourcePath: filepath.Join(root, "profile.yaml"),
		Collect: config.CollectConfig{
			OutputDir: filepath.Join(root, "collect"), DefaultFormats: []string{"json"},
			IncludeMedia: &yes, DownloadMedia: &yes, PersistMode: "index",
		},
		Gateway:      config.GatewayConfig{ArtifactEnabled: &no},
		LocalBrowser: config.LocalBrowserConfig{CookieStoreDir: filepath.Join(root, "empty-sessions")},
	}
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CollectURL(t.Context(), wechatVideoTestURL, nil, "")
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrLoginRequired ||
		!strings.Contains(readErr.Message, "digcrawl browser login --platform wechat_channels") ||
		!strings.Contains(readErr.Message, cfg.SourcePath) {
		t.Fatalf("expected login action for the active profile, got %v", err)
	}
	if _, err := os.Stat(cfg.Collect.OutputDir); !os.IsNotExist(err) || len(result.Written) != 0 {
		t.Fatalf("login failure published output: result=%+v err=%v", result, err)
	}
}

func TestCollectWechatChannelsUsesSharedArtifactExport(t *testing.T) {
	for _, tc := range []struct {
		name        string
		retention   bool
		unavailable bool
	}{
		{name: "normal export"},
		{name: "export after retention failure", retention: true},
		{name: "no export when fallback fails", retention: true, unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			yes := true
			artifacts, err := artifact.NewDiskStore(filepath.Join(root, "artifacts"), 0)
			if err != nil {
				t.Fatal(err)
			}
			clip, err := artifacts.Put(t.Context(), artifact.NewID(), "video/mp4", "video.mp4", []byte(testutil.MP4))
			if err != nil {
				t.Fatal(err)
			}
			const downloadError = "collect video after provider retention failed"
			reg := registry.New()
			reg.Register(registry.Entry{
				Descriptor: protocol.ToolDescriptor{Name: platformprov.ToolWechatChannelsRead},
				Handler: func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
					if tc.unavailable {
						return nil, nil, errors.New(downloadError)
					}
					content := (&platform.Content{
						Platform: "wechat_channels", ContentType: "video", SourceURL: wechatVideoTestURL, Title: "caption",
						Media: []platform.MediaItem{{Type: "video", ArtifactID: clip.ID}},
					}).ToMap()
					if tc.retention {
						content["platform_metadata"] = map[string]any{"video_download_status": "failed"}
						return content, nil, providerVideoRetentionTestError{}
					}
					return content, nil, nil
				},
			})
			svc := &Service{
				cfg: &config.Config{Collect: config.CollectConfig{
					OutputDir: filepath.Join(root, "collect"), DefaultFormats: []string{"json"},
					IncludeMedia: &yes, DownloadMedia: &yes, PersistMode: "index",
				}},
				rt: runtime.New(reg, runtime.Options{}), artifacts: artifacts,
			}
			result, err := svc.CollectURL(t.Context(), wechatVideoTestURL, nil, "")
			if tc.unavailable {
				if err == nil || !strings.Contains(err.Error(), downloadError) || len(result.Written) != 0 {
					t.Fatalf("failed fallback published output: result=%+v err=%v", result, err)
				}
				if _, err := os.Stat(svc.cfg.Collect.OutputDir); !os.IsNotExist(err) {
					t.Fatalf("failed fallback created an output directory: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Downloaded) != 1 {
				t.Fatalf("expected one media entry, got %v", result.Downloaded)
			}
			name, _ := result.Downloaded[0]["local"].(string)
			video, err := os.ReadFile(filepath.Join(result.Paths.Assets, name))
			if err != nil || string(video) != testutil.MP4 {
				t.Fatalf("exported MP4 content invalid: %v", err)
			}
			entry, ok, err := FindByURL(svc.cfg.Collect.OutputDir, wechatVideoTestURL)
			if err != nil || !ok || entry.Files["json"] != result.Paths.JSON {
				t.Fatalf("index should reference the document JSON: entry=%+v err=%v", entry, err)
			}
		})
	}
}
