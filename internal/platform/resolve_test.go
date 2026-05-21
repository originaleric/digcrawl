package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveHTTPFollowAllowed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/short" {
			http.Redirect(w, r, "/landing/explore/abc123def4567890", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resolved, err := ResolveHTTPFollow(context.Background(), srv.URL+"/short", []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved == "" {
		t.Fatal("empty resolved")
	}
}

func TestResolveHTTPFollowRejectHost(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	}))
	defer srv.Close()

	_, err := ResolveHTTPFollow(context.Background(), srv.URL, []string{"127.0.0.1"})
	if err == nil {
		t.Fatal("expected host rejection")
	}
}
