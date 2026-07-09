package summary

import (
	"context"
	"net/http"
	"net/http/httptest"
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
