package httpgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/watchlist"
	watchlistservice "github.com/originaleric/digcrawl/internal/watchlist/service"
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

func (s *Server) EnableWatchlistAdmin(cfg *config.Config) {
	if cfg == nil || s.authToken == "" {
		return
	}
	s.mux.HandleFunc("GET /admin/watchlist/sources", s.handleWatchlistSourcesList(cfg))
	s.mux.HandleFunc("POST /admin/watchlist/sources", s.handleWatchlistSourcesCreate(cfg))
	s.mux.HandleFunc("GET /admin/watchlist/sources/{id}", s.handleWatchlistSourcesGet(cfg))
	s.mux.HandleFunc("PUT /admin/watchlist/sources/{id}", s.handleWatchlistSourcesUpdate(cfg))
	s.mux.HandleFunc("DELETE /admin/watchlist/sources/{id}", s.handleWatchlistSourcesDelete(cfg))
	s.mux.HandleFunc("PATCH /admin/watchlist/sources/{id}/enable", s.handleWatchlistSourcesSetEnabled(cfg, true))
	s.mux.HandleFunc("PATCH /admin/watchlist/sources/{id}/disable", s.handleWatchlistSourcesSetEnabled(cfg, false))
	s.mux.HandleFunc("POST /admin/watchlist/sources/{id}/discover", s.handleWatchlistSourcesDiscover(cfg))
	s.mux.HandleFunc("POST /admin/watchlist/sources/{id}/run", s.handleWatchlistSourcesRun(cfg))
	s.mux.HandleFunc("POST /admin/watchlist/run-due", s.handleWatchlistRunDue(cfg))
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

func (s *Server) handleWatchlistSourcesList(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := watchlistservice.OpenConfiguredStore(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.Close()
		sources, err := st.ListSources(r.Context(), r.URL.Query().Get("enabled_only") == "true")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": sourceViews(sources)})
	}
}

func (s *Server) handleWatchlistSourcesGet(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := watchlistservice.OpenConfiguredStore(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.Close()
		src, err := st.GetSource(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sourceView(src))
	}
}

func (s *Server) handleWatchlistSourcesCreate(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		src, err := readWatchlistSourceRequest(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		st, err := watchlistservice.OpenConfiguredStore(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.Close()
		if err := st.UpsertSource(r.Context(), src); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": strings.TrimSpace(src.ID), "status": "saved"})
	}
}

func (s *Server) handleWatchlistSourcesUpdate(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		src, err := readWatchlistSourceRequest(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		pathID := strings.TrimSpace(r.PathValue("id"))
		if strings.TrimSpace(src.ID) == "" {
			src.ID = pathID
		}
		if strings.TrimSpace(src.ID) != pathID {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source id cannot differ from path id"})
			return
		}
		st, err := watchlistservice.OpenConfiguredStore(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.Close()
		if err := st.UpsertSource(r.Context(), src); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": pathID, "status": "saved"})
	}
}

func (s *Server) handleWatchlistSourcesSetEnabled(cfg *config.Config, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := watchlistservice.OpenConfiguredStore(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.Close()
		id := r.PathValue("id")
		if err := st.SetSourceEnabled(r.Context(), id, enabled); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
	}
}

func (s *Server) handleWatchlistSourcesDelete(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := watchlistservice.OpenConfiguredStore(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer st.Close()
		id := r.PathValue("id")
		if err := st.DeleteSource(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "deleted"})
	}
}

func (s *Server) handleWatchlistSourcesRun(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := watchlistservice.NewService(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer svc.Close()
		summary, err := svc.RunConfiguredItem(r.Context(), r.PathValue("id"))
		status := http.StatusOK
		if err != nil {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"summary": summary, "error": errorString(err)})
	}
}

func (s *Server) handleWatchlistSourcesDiscover(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := watchlistservice.NewDiscoverService(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer svc.Close()
		items, err := svc.DiscoverConfiguredItem(r.Context(), r.PathValue("id"))
		status := http.StatusOK
		if err != nil {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"count": len(items), "items": discoveredItemViews(items), "error": errorString(err)})
	}
}

func (s *Server) handleWatchlistRunDue(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := watchlistservice.NewService(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer svc.Close()
		summaries, err := svc.RunDue(r.Context(), time.Now())
		status := http.StatusOK
		if err != nil {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"summaries": summaries, "error": errorString(err)})
	}
}

type watchlistSourceRequest struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Platform     string         `json:"platform"`
	Kind         string         `json:"kind"`
	URL          string         `json:"url"`
	Enabled      *bool          `json:"enabled"`
	ScheduleMode string         `json:"schedule_mode"`
	Every        string         `json:"every"`
	NotifyTo     string         `json:"notify_to"`
	Options      map[string]any `json:"options"`
}

type watchlistSourceView struct {
	ID           string         `json:"id"`
	Name         string         `json:"name,omitempty"`
	Platform     string         `json:"platform"`
	Kind         string         `json:"kind"`
	URL          string         `json:"url"`
	Enabled      bool           `json:"enabled"`
	ScheduleMode string         `json:"schedule_mode"`
	Every        string         `json:"every"`
	NotifyTo     string         `json:"notify_to,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
}

type watchlistDiscoveredItemView struct {
	ID           string         `json:"id,omitempty"`
	ContentKey   string         `json:"content_key"`
	URL          string         `json:"url"`
	CanonicalURL string         `json:"canonical_url,omitempty"`
	Title        string         `json:"title,omitempty"`
	Author       string         `json:"author,omitempty"`
	PublishedAt  string         `json:"published_at,omitempty"`
	ContentType  string         `json:"content_type,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}

func readWatchlistSourceRequest(r *http.Request) (watchlist.WatchSource, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return watchlist.WatchSource{}, err
	}
	var req watchlistSourceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return watchlist.WatchSource{}, err
	}
	interval := 6 * time.Hour
	if strings.TrimSpace(req.Every) != "" {
		interval, err = time.ParseDuration(req.Every)
		if err != nil {
			return watchlist.WatchSource{}, err
		}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	mode := strings.TrimSpace(req.ScheduleMode)
	if mode == "" {
		mode = "interval"
	}
	return watchlist.WatchSource{
		ID: req.ID, Name: req.Name, Platform: req.Platform, Type: req.Kind, URL: req.URL,
		Enabled: enabled, ScheduleMode: mode, Interval: interval, NotifyTo: req.NotifyTo, Options: req.Options,
	}, nil
}

func sourceViews(sources []watchlist.WatchSource) []watchlistSourceView {
	out := make([]watchlistSourceView, 0, len(sources))
	for _, src := range sources {
		out = append(out, sourceView(src))
	}
	return out
}

func sourceView(src watchlist.WatchSource) watchlistSourceView {
	return watchlistSourceView{
		ID: src.ID, Name: src.Name, Platform: src.Platform, Kind: src.Type, URL: src.URL,
		Enabled: src.Enabled, ScheduleMode: src.ScheduleMode, Every: src.Interval.String(),
		NotifyTo: src.NotifyTo, Options: src.Options,
	}
}

func discoveredItemViews(items []watchlist.FeedItem) []watchlistDiscoveredItemView {
	out := make([]watchlistDiscoveredItemView, 0, len(items))
	for _, item := range items {
		out = append(out, watchlistDiscoveredItemView{
			ID: item.ID, ContentKey: item.ContentKey, URL: item.URL, CanonicalURL: item.CanonicalURL,
			Title: item.Title, Author: item.Author, PublishedAt: formatOptionalTime(item.PublishedAt),
			ContentType: item.ContentType, Raw: item.Raw,
		})
	}
	return out
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
