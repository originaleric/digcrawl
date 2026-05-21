package gateway

import (
	"context"
	"time"

	"github.com/originaleric/diggate/config"
	"github.com/originaleric/diggate/gateway/artifact"
	"github.com/originaleric/diggate/gateway/registry"
	"github.com/originaleric/diggate/gateway/runtime"
	"github.com/originaleric/diggate/provider"
	exampleprov "github.com/originaleric/diggate/provider/example"
)

// RegistryOptions configures tool registration.
type RegistryOptions struct {
	Providers        []provider.Provider
	ConfigDomains    []string
	AllowedReadPaths []string
	ArtifactStore    artifact.Store
}

// NewRegistry builds a gateway tool registry from config and providers.
func NewRegistry(cfg *config.Config, opts RegistryOptions) (*registry.Registry, error) {
	reg := registry.New()
	providers := opts.Providers
	if len(providers) == 0 {
		providers = DefaultProviders(cfg, opts)
	}
	if err := provider.Load(context.Background(), reg, providers...); err != nil {
		return nil, err
	}
	return reg, nil
}

// DefaultProviders returns built-in providers based on config.
func DefaultProviders(cfg *config.Config, opts RegistryOptions) []provider.Provider {
	var out []provider.Provider
	if cfg.Providers.ExampleEnabled == nil || *cfg.Providers.ExampleEnabled {
		readPaths := opts.AllowedReadPaths
		if len(readPaths) == 0 {
			readPaths = cfg.Gateway.AllowedReadPaths
		}
		out = append(out, &exampleprov.Provider{AllowedReadPaths: readPaths})
	}
	return out
}

// NewArtifactStore creates a disk artifact store from gateway config.
func NewArtifactStore(cfg *config.Config) (artifact.Store, error) {
	dir := cfg.Gateway.ArtifactDir
	if dir == "" {
		dir = "storage/diggate/artifacts"
	}
	ttl := time.Duration(cfg.Gateway.ArtifactTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = time.Hour
	}
	return artifact.NewDiskStore(dir, ttl)
}

func runtimeOptions(cfg *config.Config, instanceID string, allowedTools []string, store artifact.Store) runtime.Options {
	domains := cfg.Gateway.AllowedDomains
	return runtime.Options{
		InstanceID:    instanceID,
		AllowedTools:  allowedTools,
		ConfigDomains: domains,
		ArtifactStore: store,
	}
}

// NewRuntime creates a runtime with registry and gateway options from config.
func NewRuntime(cfg *config.Config) (*runtime.Runtime, error) {
	gw := cfg.Gateway
	instanceID := gw.InstanceID
	if instanceID == "" {
		instanceID = "diggate-local"
	}
	var store artifact.Store
	if gw.ArtifactEnabled == nil || *gw.ArtifactEnabled {
		if s, err := NewArtifactStore(cfg); err == nil {
			store = s
		}
	}
	reg, err := NewRegistry(cfg, RegistryOptions{ArtifactStore: store})
	if err != nil {
		return nil, err
	}
	return runtime.New(reg, runtimeOptions(cfg, instanceID, gw.AllowedTools, store)), nil
}

// NewCollectorRuntime creates a runtime for the local Collector process.
func NewCollectorRuntime(cfg *config.Config) (*runtime.Runtime, error) {
	col := cfg.Collector
	instanceID := col.InstanceID
	if instanceID == "" {
		instanceID = cfg.Gateway.InstanceID
	}
	if instanceID == "" {
		instanceID = "diggate-collector"
	}
	allowed := col.AllowedTools
	if len(allowed) == 0 {
		allowed = cfg.Gateway.AllowedTools
	}
	var store artifact.Store
	if s, err := NewArtifactStore(cfg); err == nil {
		store = s
	}
	reg, err := NewRegistry(cfg, RegistryOptions{ArtifactStore: store})
	if err != nil {
		return nil, err
	}
	return runtime.New(reg, runtimeOptions(cfg, instanceID, allowed, store)), nil
}
