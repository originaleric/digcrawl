//go:build !digflow_embedded

package summary

import (
	"context"
	"fmt"
	"time"
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
}

func (p *EmbeddedDigFlowProvider) Summarize(ctx context.Context, req Request) (*Result, error) {
	req = ApplyBudget(req, p.Budget)
	return p.fallback(ctx, req, fmt.Errorf("embedded digflow runtime is disabled; rebuild with -tags digflow_embedded"))
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
