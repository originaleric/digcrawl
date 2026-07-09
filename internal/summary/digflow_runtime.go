package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
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
	loadGlobalTools := false
	opts := digflow.Options{
		RootDir:           expandPath(p.RootDir),
		ConfigPath:        expandPath(p.ConfigPath),
		GormConfigPath:    expandPath(p.GormConfigPath),
		EinoConfigPath:    expandPath(p.EinoConfigPath),
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
