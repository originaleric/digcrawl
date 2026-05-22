package httpgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

// Server is the HTTP Tool Gateway for DigEino.
type Server struct {
	rt        *runtime.Runtime
	artStore  artifact.Store
	authToken string
	mux       *http.ServeMux
}

// NewServer creates an HTTP gateway server.
func NewServer(rt *runtime.Runtime, artStore artifact.Store, authToken string) *Server {
	s := &Server{rt: rt, artStore: artStore, authToken: strings.TrimSpace(authToken), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /manifest", s.handleManifest)
	s.mux.HandleFunc("POST /tools/call", s.handleToolCall)
	s.mux.HandleFunc("GET /artifacts/{id}", s.handleArtifact)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.authToken != "" && !s.authenticated(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authenticated(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.authToken
	}
	return r.Header.Get("X-DigCrawl-Token") == s.authToken
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.rt.Manifest())
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	if s.artStore == nil {
		http.Error(w, "artifact store disabled", http.StatusNotFound)
		return
	}
	id := strings.TrimPrefix(r.PathValue("id"), "/")
	data, contentType, err := s.artStore.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	var call protocol.ToolCall
	if err := json.Unmarshal(body, &call); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if call.Type == "" {
		call.Type = protocol.TypeToolCall
	}
	result := s.rt.Execute(r.Context(), &call)
	status := http.StatusOK
	if result.Status == "error" && result.Error != nil {
		switch result.Error.Code {
		case "INVALID_INPUT", "TOOL_NOT_ALLOWED":
			status = http.StatusBadRequest
		case "DOMAIN_NOT_ALLOWED":
			status = http.StatusForbidden
		}
	}
	writeJSON(w, status, result)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Shutdown is a placeholder for graceful shutdown hooks.
func (s *Server) Shutdown(ctx context.Context) error {
	_ = ctx
	return errors.New("not implemented")
}
