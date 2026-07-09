package summary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDigFlowProviderFallsBackOnInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"not summary json"}`))
	}))
	defer srv.Close()
	p := DigFlowProvider{
		BaseURL: srv.URL, Endpoint: "/", Agent: "agent", Timeout: time.Second,
		Fallback: ExtractiveProvider{},
	}
	res, err := p.Summarize(context.Background(), Request{
		WatchID: "w1", Platform: "rss", SourceName: "src",
		Items: []Item{{ItemID: "i1", URL: "https://example.com/a", Title: "A", Text: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != "extractive" || !strings.Contains(res.Error, "schema") {
		t.Fatalf("expected extractive fallback with schema error, got %+v", res)
	}
}

func TestDigFlowProviderReportsHTTPProviderName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"digest_title":"t","items":[{"title":"a"}]}`))
	}))
	defer srv.Close()
	p := DigFlowProvider{
		BaseURL: srv.URL, Endpoint: "/", Agent: "agent", Timeout: time.Second,
		Fallback: ExtractiveProvider{},
	}
	res, err := p.Summarize(context.Background(), Request{
		WatchID: "w1", Platform: "rss", SourceName: "src",
		Items: []Item{{ItemID: "i1", URL: "https://example.com/a", Title: "A", Text: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != "digflow_http" {
		t.Fatalf("Provider=%q, want digflow_http", res.Provider)
	}
}

func TestParseOpenAIChatSummary(t *testing.T) {
	data := []byte(`{"choices":[{"message":{"content":"{\"digest_title\":\"t\",\"items\":[{\"title\":\"a\"}]}"}}]}`)
	res, err := parseOpenAIChatSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.DigestTitle != "t" || len(res.Items) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestSafeProviderErrorRedactsSensitiveBody(t *testing.T) {
	err := safeProviderError("digflow", 500, []byte(`{"authorization":"Bearer secret-token"}`))
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked sensitive body: %v", err)
	}
}

func TestSafeFallbackErrorRedactsRuntimeDetails(t *testing.T) {
	got := safeFallbackError(context.DeadlineExceeded)
	if got != "summary provider unavailable" {
		t.Fatalf("unexpected timeout diagnostic: %q", got)
	}
	got = safeFallbackError(errString("connect root:secret@tcp(127.0.0.1:3306)/dig_crawl failed"))
	if strings.Contains(got, "secret") || strings.Contains(got, "127.0.0.1") {
		t.Fatalf("diagnostic leaked runtime detail: %q", got)
	}
}

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

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

type errString string

func (e errString) Error() string { return string(e) }
