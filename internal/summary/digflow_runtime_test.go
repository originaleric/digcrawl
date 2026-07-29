//go:build digflow_embedded

package summary

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEmbeddedDigFlowManifestLoads(t *testing.T) {
	root := repoRoot(t)
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()
	configPath := filepath.Join(tmp, "digflow.yml")
	einoPath := filepath.Join(tmp, "eino.yml")
	if err := os.WriteFile(configPath, []byte(`AppDebug: true
Logs:
  OutputPaths: ["stdout"]
  ErrorOutputPaths: ["stderr"]
  Level: "error"
  TextFormat: "console"
ChatHistoryStore:
  Enabled: false
  Type: "memory"
sql_lite:
  Path: ""
long_term_memory:
  Table: "df_long_term_memory"
LocalMemory:
  TableNamePrefix: "df_"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(einoPath, []byte(`version: 2
digeino:
  Status:
    Store:
      Enabled: false
  Learning:
    Enabled: false
context:
  Enabled: false
Apps:
  AutoLoadAll: false
  Whitelist:
    - "watchlist_summary_agent"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &EmbeddedDigFlowProvider{
		RootDir:           root,
		ConfigPath:        configPath,
		EinoConfigPath:    einoPath,
		ManifestPath:      "agents/watchlist/agents.yml",
		DisableCheckpoint: true,
	}
	if _, err := p.ensureRuntime(context.Background()); err != nil {
		t.Fatalf("embedded digflow manifest did not load: %v", err)
	}
}

func TestPrepareRuntimeConfigFilesExpandsEnvTemplates(t *testing.T) {
	t.Setenv("DIGCRAWL_TEST_HOST", "127.0.0.1")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "digflow.yml")
	gormPath := filepath.Join(dir, "digflow-gorm.yml")
	einoPath := filepath.Join(dir, "eino.yml")
	if err := os.WriteFile(cfgPath, []byte("AppDebug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gormPath, []byte("Host: \"${DIGCRAWL_TEST_HOST}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(einoPath, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &EmbeddedDigFlowProvider{
		ConfigPath:     cfgPath,
		GormConfigPath: gormPath,
		EinoConfigPath: einoPath,
	}
	prepared, cleanup, err := p.prepareRuntimeConfigFiles()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(prepared.GormConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Host: \"127.0.0.1\"\n" {
		t.Fatalf("template was not expanded: %q", string(data))
	}
	if prepared.GormConfigPath == gormPath {
		t.Fatal("expected expanded config to be written to a temp file")
	}
}

func TestPrepareRuntimeConfigFilesRejectsMissingEnvTemplates(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "digflow.yml")
	gormPath := filepath.Join(dir, "digflow-gorm.yml")
	einoPath := filepath.Join(dir, "eino.yml")
	if err := os.WriteFile(cfgPath, []byte("AppDebug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gormPath, []byte("Host: \"${DIGCRAWL_MISSING_TEMPLATE_ENV}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(einoPath, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &EmbeddedDigFlowProvider{
		ConfigPath:     cfgPath,
		GormConfigPath: gormPath,
		EinoConfigPath: einoPath,
	}
	_, cleanup, err := p.prepareRuntimeConfigFiles()
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil {
		t.Fatal("expected missing template env to fail")
	}
	if !strings.Contains(err.Error(), "DIGCRAWL_MISSING_TEMPLATE_ENV") {
		t.Fatalf("missing env error should name the variable, got %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
