//go:build digflow_embedded

package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/originaleric/DigFlow/pkg/digflow"
)

type EmbeddedDigFlowProvider struct {
	RootDir           string
	ConfigPath        string
	GormConfigPath    string
	EinoConfigPath    string
	ManifestPath      string
	GlobalToolsPath   string
	GlobalSkillsDir   string
	Agent             string
	DisableCheckpoint bool
	Timeout           time.Duration
	Budget            InputBudget
	Fallback          Provider

	mu      sync.Mutex
	runtime *digflow.Runtime
}

func (p *EmbeddedDigFlowProvider) Summarize(ctx context.Context, req Request) (*Result, error) {
	req = ApplyBudget(req, p.Budget)
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rt, err := p.ensureRuntime(cctx)
	if err != nil {
		return p.fallback(cctx, req, err)
	}
	agent := os.ExpandEnv(strings.TrimSpace(p.Agent))
	if agent == "" {
		return p.fallback(cctx, req, fmt.Errorf("digflow agent is required"))
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return p.fallback(cctx, req, err)
	}
	resp, err := rt.Invoke(cctx, agent, digflow.InvokeRequest{
		SessionID: stableSessionID(req),
		UserID:    "digcrawl",
		Channel:   "watchlist",
		Messages: []digflow.Message{{
			Role:    "user",
			Content: string(reqData),
		}},
		Metadata: map[string]interface{}{
			"source":      "digcrawl_watchlist",
			"watch_id":    req.WatchID,
			"platform":    req.Platform,
			"source_name": req.SourceName,
		},
	})
	if err != nil {
		return p.fallback(cctx, req, err)
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return p.fallback(cctx, req, fmt.Errorf("digflow response has no content"))
	}
	res, err := parseSummaryResult([]byte(resp.Message.Content))
	if err != nil {
		return p.fallback(cctx, req, err)
	}
	res.Provider = "digflow_runtime"
	return res, nil
}

func (p *EmbeddedDigFlowProvider) ensureRuntime(ctx context.Context) (*digflow.Runtime, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.runtime != nil {
		return p.runtime, nil
	}
	prepared, cleanup, err := p.prepareRuntimeConfigFiles()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	loadGlobalTools := false
	opts := digflow.Options{
		RootDir:           expandPath(p.RootDir),
		ConfigPath:        prepared.ConfigPath,
		GormConfigPath:    prepared.GormConfigPath,
		EinoConfigPath:    prepared.EinoConfigPath,
		GlobalSkillsDir:   expandPath(p.GlobalSkillsDir),
		LoadGlobalTools:   &loadGlobalTools,
		DisableCheckpoint: p.DisableCheckpoint,
	}
	if path := expandPath(p.GlobalToolsPath); strings.TrimSpace(path) != "" {
		opts.GlobalToolsLoad = digflow.HostGlobalToolsYAML(path)
	}
	rt, err := digflow.NewRuntime(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("init embedded digflow runtime: %w", err)
	}
	if manifest := expandPath(p.ManifestPath); strings.TrimSpace(manifest) != "" {
		if err := rt.LoadManifest(ctx, manifest); err != nil {
			return nil, fmt.Errorf("load digflow manifest %q: %w", manifest, err)
		}
	}
	p.runtime = rt
	return rt, nil
}

type preparedRuntimeConfigFiles struct {
	ConfigPath     string
	GormConfigPath string
	EinoConfigPath string
}

func (p *EmbeddedDigFlowProvider) prepareRuntimeConfigFiles() (preparedRuntimeConfigFiles, func(), error) {
	prepared := preparedRuntimeConfigFiles{
		ConfigPath:     expandPath(p.ConfigPath),
		GormConfigPath: expandPath(p.GormConfigPath),
		EinoConfigPath: expandPath(p.EinoConfigPath),
	}
	paths := []string{prepared.ConfigPath, prepared.GormConfigPath, prepared.EinoConfigPath}
	needsTemplate := false
	for _, path := range paths {
		if configFileNeedsEnvExpansion(path) {
			needsTemplate = true
			break
		}
	}
	if !needsTemplate {
		return prepared, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "digcrawl-digflow-config-*")
	if err != nil {
		return preparedRuntimeConfigFiles{}, nil, fmt.Errorf("create digflow config temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	var copyErr error
	if prepared.ConfigPath, copyErr = expandConfigTemplateToDir(prepared.ConfigPath, dir); copyErr != nil {
		cleanup()
		return preparedRuntimeConfigFiles{}, nil, copyErr
	}
	if prepared.GormConfigPath, copyErr = expandConfigTemplateToDir(prepared.GormConfigPath, dir); copyErr != nil {
		cleanup()
		return preparedRuntimeConfigFiles{}, nil, copyErr
	}
	if prepared.EinoConfigPath, copyErr = expandConfigTemplateToDir(prepared.EinoConfigPath, dir); copyErr != nil {
		cleanup()
		return preparedRuntimeConfigFiles{}, nil, copyErr
	}
	return prepared, cleanup, nil
}

func configFileNeedsEnvExpansion(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "${")
}

func expandConfigTemplateToDir(path, dir string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read digflow config template %q: %w", path, err)
	}
	expanded, err := expandRequiredEnvTemplate(string(data))
	if err != nil {
		return "", fmt.Errorf("expand digflow config template %q: %w", path, err)
	}
	name := filepath.Base(path)
	if strings.TrimSpace(name) == "" || name == "." || name == string(filepath.Separator) {
		name = "config.yml"
	}
	out := filepath.Join(dir, name)
	if err := os.WriteFile(out, []byte(expanded), 0o600); err != nil {
		return "", fmt.Errorf("write expanded digflow config %q: %w", out, err)
	}
	return out, nil
}

func expandRequiredEnvTemplate(s string) (string, error) {
	missing := map[string]struct{}{}
	expanded := os.Expand(s, func(name string) string {
		value, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(value) == "" {
			missing[name] = struct{}{}
			return ""
		}
		return value
	})
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", fmt.Errorf("missing required environment variables: %s", strings.Join(names, ", "))
	}
	return expanded, nil
}

func (p *EmbeddedDigFlowProvider) fallback(ctx context.Context, req Request, cause error) (*Result, error) {
	if p.Fallback == nil {
		return nil, cause
	}
	res, err := p.Fallback.Summarize(ctx, req)
	if res != nil {
		res.Error = safeFallbackError(cause)
	}
	return res, err
}

func expandPath(v string) string {
	return os.ExpandEnv(strings.TrimSpace(v))
}

func stableSessionID(req Request) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(req.WatchID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(req.RunTime))
	return fmt.Sprintf("digcrawl-watchlist-%x", h.Sum64())
}
