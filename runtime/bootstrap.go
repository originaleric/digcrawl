package runtime

import (
	"context"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/provider"
	browserprov "github.com/originaleric/digcrawl/provider/browser"
	exampleprov "github.com/originaleric/digcrawl/provider/example"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	stubprov "github.com/originaleric/digcrawl/provider/stub"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
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
	return append([]provider.Provider{}, collectProviders(cfg, opts)...)
}

func collectProviders(cfg *config.Config, opts RegistryOptions) []provider.Provider {
	domains := opts.ConfigDomains
	if len(domains) == 0 {
		domains = cfg.MergedAllowedDomains()
	}
	var out []provider.Provider
	if cfg.Providers.PlatformEnabled == nil || *cfg.Providers.PlatformEnabled {
		out = append(out, &platformprov.Provider{
			ConfigDomains: domains,
			ArtifactStore: opts.ArtifactStore,
		})
	}
	if browserProviderEnabled(cfg) {
		out = append(out, &browserprov.Provider{
			ConfigDomains: domains,
			ArtifactStore: opts.ArtifactStore,
		})
	}
	if cfg.Providers.ExampleEnabled == nil || *cfg.Providers.ExampleEnabled {
		readPaths := opts.AllowedReadPaths
		if len(readPaths) == 0 {
			readPaths = cfg.Gateway.AllowedReadPaths
		}
		out = append(out, &exampleprov.Provider{AllowedReadPaths: readPaths})
	}
	if cfg.Providers.StubEnabled != nil && *cfg.Providers.StubEnabled {
		out = append(out, &stubprov.Provider{})
	}
	return out
}

func browserProviderEnabled(cfg *config.Config) bool {
	if cfg.Providers.BrowserEnabled != nil && !*cfg.Providers.BrowserEnabled {
		return false
	}
	lb := cfg.LocalBrowser
	if lb.Enabled != nil && !*lb.Enabled {
		return false
	}
	return cfg.Providers.BrowserEnabled == nil || *cfg.Providers.BrowserEnabled
}

// NewArtifactStore creates a disk artifact store from gateway config.
func NewArtifactStore(cfg *config.Config) (artifact.Store, error) {
	dir := cfg.Gateway.ArtifactDir
	if dir == "" {
		dir = "storage/digcrawl/artifacts"
	}
	ttl := time.Duration(cfg.Gateway.ArtifactTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = time.Hour
	}
	return artifact.NewDiskStore(dir, ttl)
}

func runtimeOptions(cfg *config.Config, instanceID string, allowedTools []string, store artifact.Store) Options {
	domains := cfg.Gateway.AllowedDomains
	return Options{
		InstanceID:      instanceID,
		AllowedTools:    allowedTools,
		ConfigDomains:   domains,
		ArtifactStore:   store,
		RequireApproval: cfg.Gateway.RequireApproval == nil || *cfg.Gateway.RequireApproval,
	}
}

// NewRuntime creates a runtime with registry and gateway options from config.
func NewRuntime(cfg *config.Config) (*Runtime, error) {
	gw := cfg.Gateway
	instanceID := gw.InstanceID
	if instanceID == "" {
		instanceID = "digcrawl-local"
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
	return New(reg, runtimeOptions(cfg, instanceID, gw.AllowedTools, store)), nil
}

// NewCollectRuntime creates a runtime for local content collect (platform tools).
func NewCollectRuntime(cfg *config.Config) (*Runtime, error) {
	instanceID := "digcrawl-collect"
	var store artifact.Store
	if cfg.Gateway.ArtifactEnabled == nil || *cfg.Gateway.ArtifactEnabled {
		if s, err := NewArtifactStore(cfg); err == nil {
			store = s
		}
	}
	domains := cfg.MergedAllowedDomains()
	providers := collectProviders(cfg, RegistryOptions{
		ConfigDomains: domains,
		ArtifactStore: store,
	})
	reg, err := NewRegistry(cfg, RegistryOptions{
		Providers:     providers,
		ConfigDomains: domains,
		ArtifactStore: store,
	})
	if err != nil {
		return nil, err
	}
	opts := runtimeOptions(cfg, instanceID, nil, store)
	opts.RequireApproval = false
	return New(reg, opts), nil
}

// NewCollectorRuntime creates a runtime for the local Collector process.
func NewCollectorRuntime(cfg *config.Config) (*Runtime, error) {
	col := cfg.Collector
	instanceID := col.InstanceID
	if instanceID == "" {
		instanceID = cfg.Gateway.InstanceID
	}
	if instanceID == "" {
		instanceID = "digcrawl-collector"
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
	return New(reg, runtimeOptions(cfg, instanceID, allowed, store)), nil
}
