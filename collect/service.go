package collect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/diggate/config"
	"github.com/originaleric/diggate/runtime"
	"github.com/originaleric/diggate/runtime/artifact"
)

// Service runs local content collect and export.
type Service struct {
	cfg   *config.Config
	rt    *runtime.Runtime
	store artifact.Store
}

// NewService builds a collect service with platform and browser tools.
func NewService(cfg *config.Config) (*Service, error) {
	rt, err := runtime.NewCollectRuntime(cfg)
	if err != nil {
		return nil, err
	}
	var store artifact.Store
	if cfg.Gateway.ArtifactEnabled == nil || *cfg.Gateway.ArtifactEnabled {
		if s, err := runtime.NewArtifactStore(cfg); err == nil {
			store = s
		}
	}
	return &Service{cfg: cfg, rt: rt, store: store}, nil
}

func (s *Service) exportOpts(outDir string, formats []string) ExportOptions {
	download := s.cfg.Collect.DownloadMedia != nil && *s.cfg.Collect.DownloadMedia
	return ExportOptions{
		OutputDir:       outDir,
		Formats:         formats,
		DownloadMedia:   download,
		MaxMediaBytes:   s.cfg.Collect.MaxMediaBytes,
		MediaTimeoutSec: s.cfg.Collect.MediaTimeoutSec,
		ArtifactStore: s.store,
	}
}

func (s *Service) outputRoot(outDir string) string {
	if strings.TrimSpace(outDir) == "" {
		return s.cfg.Collect.OutputDir
	}
	return outDir
}

func (s *Service) throttle() {
	if s.cfg.Collect.RateLimitMs <= 0 {
		return
	}
	time.Sleep(time.Duration(s.cfg.Collect.RateLimitMs) * time.Millisecond)
}

// CollectURL fetches one URL and exports to disk.
func (s *Service) CollectURL(ctx context.Context, url string, formats []string, outDir string) (ExportResult, error) {
	if len(formats) == 0 {
		formats = s.cfg.Collect.DefaultFormats
	}
	root := s.outputRoot(outDir)

	match, err := Detect(url)
	if err != nil {
		return ExportResult{}, err
	}

	out, _, err := Run(ctx, s.rt, RunOptions{
		URL:          url,
		Formats:      formats,
		IncludeMedia: s.cfg.Collect.IncludeMedia == nil || *s.cfg.Collect.IncludeMedia,
		Match:        match,
	})
	if err != nil {
		return ExportResult{}, err
	}

	result, err := Export(ctx, out, s.exportOpts(root, formats))
	if err != nil {
		return ExportResult{}, err
	}

	if s.cfg.Collect.IndexEnabled == nil || *s.cfg.Collect.IndexEnabled {
		entry := EntryFromExport(url, match.Platform, out, result.Paths, result.Written)
		if err := UpsertEntry(root, entry); err != nil {
			return result, err
		}
		result.Written = append(result.Written, IndexPath(root))
	}
	return result, nil
}

// CollectFile reads URLs from a text file (one per line) and collects each.
func (s *Service) CollectFile(ctx context.Context, path string, formats []string, outDir string) ([]ExportResult, error) {
	lines, err := readURLLines(path)
	if err != nil {
		return nil, err
	}
	var results []ExportResult
	var firstErr error
	for i, line := range lines {
		if i > 0 {
			s.throttle()
		}
		r, err := s.CollectURL(ctx, line, formats, outDir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", line, err)
			}
			continue
		}
		results = append(results, r)
	}
	if len(results) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return results, firstErr
}

// ProcessInbox collects all pending files in the configured inbox directory.
func (s *Service) ProcessInbox(ctx context.Context, formats []string, outDir string) ([]ExportResult, error) {
	inbox := NewInbox(s.cfg.Collect.InboxDir)
	return inbox.ProcessOnce(ctx, s, formats, outDir)
}

// WatchInbox polls inbox until ctx is done.
func (s *Service) WatchInbox(ctx context.Context, formats []string, outDir string) error {
	inbox := NewInbox(s.cfg.Collect.InboxDir)
	interval := s.cfg.Collect.InboxPollInterval
	if interval <= 0 {
		interval = 5
	}
	return inbox.Watch(ctx, s, formats, outDir, time.Duration(interval)*time.Second)
}
