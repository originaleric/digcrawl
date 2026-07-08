package asr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
)

func TestAPIProviderTranscribe(t *testing.T) {
	var sawAuth bool
	var sawModel bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer test-key" {
			sawAuth = true
		}
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") == "whisper-test" && r.FormValue("response_format") == "verbose_json" {
			sawModel = true
		}
		_, _ = w.Write([]byte(`{"text":"hello world","language":"en","duration":1.2,"segments":[{"start":0,"end":1.2,"text":"hello world"}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &apiProvider{
		apiKey:         "test-key",
		baseURL:        srv.URL,
		model:          "whisper-test",
		responseFormat: "verbose_json",
		timeout:        5 * time.Second,
		maxAudioBytes:  1024,
	}
	result, err := p.Transcribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello world" || len(result.Segments) != 1 || !sawAuth || !sawModel {
		t.Fatalf("result=%+v sawAuth=%v sawModel=%v", result, sawAuth, sawModel)
	}
}

func TestAPIProviderTranscribeTextResponseFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("response_format") != "text" {
			t.Fatalf("response_format=%q", r.FormValue("response_format"))
		}
		_, _ = w.Write([]byte("plain transcript"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &apiProvider{
		apiKey:         "test-key",
		baseURL:        srv.URL,
		model:          "whisper-test",
		responseFormat: "text",
		timeout:        5 * time.Second,
		maxAudioBytes:  1024,
	}
	result, err := p.Transcribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "plain transcript" || len(result.Segments) != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestNewProviderRejectsUnsupportedAPIResponseFormat(t *testing.T) {
	_, err := newProvider(config.ASRConfig{
		Provider:       "api",
		APIKey:         "test-key",
		ResponseFormat: "xml",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported ASR response format") {
		t.Fatalf("expected unsupported response format error, got %v", err)
	}
}

func TestAPIProviderRejectsLargeAudio(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &apiProvider{maxAudioBytes: 1}
	_, err := p.Transcribe(context.Background(), audio)
	if err == nil || !strings.Contains(err.Error(), "MaxAudioBytes") {
		t.Fatalf("expected max bytes error, got %v", err)
	}
}
