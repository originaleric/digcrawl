package collect

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/originaleric/digcrawl/collect/store"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

// Service runs local content collect and export.
type Service struct {
	cfg       *config.Config
	rt        *runtime.Runtime
	artifacts artifact.Store
	collectDB store.Store

	// Output paths and index.json are shared, mutable filesystem state. Keep
	// only path reservation and index updates serialized; media download, OCR,
	// and ASR may proceed concurrently after a path has been reserved.
	outputMu           sync.Mutex
	reservedOutputPath map[string]struct{}
	indexMu            sync.Mutex

	rateMu      sync.Mutex
	nextAllowed map[string]time.Time
}

// NewService builds a collect service with platform and browser tools.
func NewService(cfg *config.Config) (*Service, error) {
	rt, err := runtime.NewCollectRuntime(cfg)
	if err != nil {
		return nil, err
	}
	var artifacts artifact.Store
	if cfg.Gateway.ArtifactEnabled == nil || *cfg.Gateway.ArtifactEnabled {
		if s, err := runtime.NewArtifactStore(cfg); err == nil {
			artifacts = s
		}
	}
	var collectDB store.Store
	if cfg.Collect.UseSQLite() {
		db, err := store.OpenSQLite(cfg.Collect.DBPathOrDefault())
		if err != nil {
			return nil, fmt.Errorf("open collect db: %w", err)
		}
		collectDB = db
	}
	return &Service{cfg: cfg, rt: rt, artifacts: artifacts, collectDB: collectDB}, nil
}

func (s *Service) exportOpts(outDir string, formats []string) ExportOptions {
	download := s.cfg.Collect.DownloadMedia != nil && *s.cfg.Collect.DownloadMedia
	return ExportOptions{
		OutputDir:       outDir,
		Formats:         formats,
		DownloadMedia:   download,
		MaxMediaBytes:   s.cfg.Collect.MaxMediaBytes,
		MediaTimeoutSec: s.cfg.Collect.MediaTimeoutSec,
		ArtifactStore:   s.artifacts,
		OCR:             s.cfg.OCR,
		ASR:             s.cfg.ASR,
	}
}

func (s *Service) outputRoot(outDir string) string {
	if strings.TrimSpace(outDir) == "" {
		return s.cfg.Collect.OutputDir
	}
	return outDir
}

func (s *Service) platformRateLimit(platform string) time.Duration {
	ms := s.cfg.Collect.RateLimitMs
	if s.cfg.Collect.PlatformRateLimits != nil {
		if v, ok := s.cfg.Collect.PlatformRateLimits[strings.ToLower(strings.TrimSpace(platform))]; ok && v > 0 {
			ms = v
		}
	}
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func (s *Service) reservePlatformSlot(platform string, now time.Time) time.Time {
	interval := s.platformRateLimit(platform)
	if interval <= 0 {
		return now
	}
	key := strings.ToLower(strings.TrimSpace(platform))
	if key == "" {
		key = "default"
	}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if s.nextAllowed == nil {
		s.nextAllowed = make(map[string]time.Time)
	}
	slot := s.nextAllowed[key]
	if slot.Before(now) {
		slot = now
	}
	s.nextAllowed[key] = slot.Add(interval)
	return slot
}

func (s *Service) waitForPlatformRateLimit(ctx context.Context, platform string) error {
	slot := s.reservePlatformSlot(platform, time.Now())
	delay := time.Until(slot)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) collectTimeoutMs() int {
	totalSec := s.cfg.LocalBrowser.TotalTimeoutSec
	if totalSec <= 0 {
		totalSec = 90
	}
	// Keep runtime's outer budget larger than browser navigation/selector work
	// plus explicit media acquisition. Export runs after Runtime.Execute and has
	// the caller's context, so it is not included here.
	if s.cfg.Collect.DownloadMedia != nil && *s.cfg.Collect.DownloadMedia && (s.cfg.Collect.IncludeMedia == nil || *s.cfg.Collect.IncludeMedia) {
		totalSec += s.cfg.Collect.MediaTimeoutSec
	}
	return (totalSec + 30) * 1000
}

// CollectURL fetches one URL and exports to disk.
func (s *Service) CollectURL(ctx context.Context, url string, formats []string, outDir string) (ExportResult, error) {
	return s.CollectURLWithOptions(ctx, url, formats, outDir, CollectURLOptions{})
}

type CollectURLOptions struct {
	UseCookieDomain string
}

// CollectURLWithOptions fetches one URL and exports to disk with runtime read options.
func (s *Service) CollectURLWithOptions(ctx context.Context, url string, formats []string, outDir string, opts CollectURLOptions) (ExportResult, error) {
	if len(formats) == 0 {
		formats = s.cfg.Collect.DefaultFormats
	}
	root := s.outputRoot(outDir)

	match, err := Detect(url)
	if err != nil {
		return ExportResult{}, err
	}
	if err := s.waitForPlatformRateLimit(ctx, match.Platform); err != nil {
		return ExportResult{}, err
	}

	out, _, err := Run(ctx, s.rt, RunOptions{
		URL:             url,
		Formats:         formats,
		IncludeMedia:    s.cfg.Collect.IncludeMedia == nil || *s.cfg.Collect.IncludeMedia,
		Match:           match,
		TimeoutMs:       s.collectTimeoutMs(),
		UseCookieDomain: strings.TrimSpace(opts.UseCookieDomain),
	})
	if err != nil {
		return ExportResult{}, err
	}

	result, err := s.export(ctx, out, root, formats)
	if err != nil {
		return ExportResult{}, err
	}

	if err := s.persistCollect(ctx, url, match, out, &result, root); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) export(ctx context.Context, out map[string]any, root string, formats []string) (ExportResult, error) {
	opts, paths, err := prepareExport(out, s.exportOpts(root, formats))
	if err != nil {
		return ExportResult{}, err
	}
	paths, release, err := s.reserveOutputPaths(paths, opts.Formats)
	if err != nil {
		return ExportResult{}, err
	}
	defer release()
	return exportToPaths(ctx, out, opts, paths)
}

func (s *Service) reserveOutputPaths(paths OutputPaths, formats []string) (OutputPaths, func(), error) {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	if s.reservedOutputPath == nil {
		s.reservedOutputPath = make(map[string]struct{})
	}
	for i := 0; i < 1000; i++ {
		try := paths
		if i > 0 {
			suffix := fmt.Sprintf("_%d", i)
			base := paths.BaseName + suffix
			try = OutputPaths{
				Dir:      paths.Dir,
				BaseName: base,
				Markdown: filepath.Join(paths.Dir, base+".md"),
				HTML:     filepath.Join(paths.Dir, base+".html"),
				JSON:     filepath.Join(paths.Dir, base+".json"),
				Assets:   filepath.Join(paths.Dir, base+".assets"),
			}
		}
		key := try.Dir + "\x00" + try.BaseName
		if _, reserved := s.reservedOutputPath[key]; reserved || anyTargetExists(try, formats) {
			continue
		}
		s.reservedOutputPath[key] = struct{}{}
		release := func() {
			s.outputMu.Lock()
			delete(s.reservedOutputPath, key)
			s.outputMu.Unlock()
		}
		return try, release, nil
	}
	return OutputPaths{}, nil, fmt.Errorf("too many concurrent exports for %q", paths.BaseName)
}

func (s *Service) persistCollect(ctx context.Context, url string, match Match, out map[string]any, result *ExportResult, root string) error {
	if s.cfg.Collect.UseIndexJSON() {
		s.indexMu.Lock()
		defer s.indexMu.Unlock()
		entry := EntryFromExport(url, match.Platform, out, result.Paths, result.Written)
		if err := UpsertEntry(root, entry); err != nil {
			return err
		}
		result.Written = append(result.Written, IndexPath(root))
	}
	if s.cfg.Collect.UseSQLite() && s.collectDB != nil {
		in := UpsertInputFromExport(url, match, out, result.Paths, result.Downloaded, root)
		if _, err := s.collectDB.Upsert(ctx, in); err != nil {
			return fmt.Errorf("sqlite upsert: %w", err)
		}
	}
	return nil
}

// CollectFile reads URLs from a text file (one per line) and collects each.
func (s *Service) CollectFile(ctx context.Context, path string, formats []string, outDir string) ([]ExportResult, error) {
	lines, err := readURLLines(path)
	if err != nil {
		return nil, err
	}
	var results []ExportResult
	var firstErr error
	for _, line := range lines {
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
