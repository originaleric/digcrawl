package devhost

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/originaleric/diggate/gateway/protocol"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server is a minimal reference host for local Collector development (not production Knowledge).
type Server struct {
	Token   string
	WSPath  string
	log     *log.Logger
	mu      sync.Mutex
	clients map[string]*clientSession
}

type clientSession struct {
	instanceID string
	conn       *websocket.Conn
	queue      []protocol.ToolCall
}

// NewServer creates a dev reference host.
func NewServer(token, wsPath string) *Server {
	if wsPath == "" {
		wsPath = "/diggate/v1/collector/ws"
	}
	return &Server{
		Token:   token,
		WSPath:  wsPath,
		log:     log.Default(),
		clients: make(map[string]*clientSession),
	}
}

// Handler returns an http.Handler for the dev host (WS + admin API).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc(s.WSPath, s.handleWS)
	mux.HandleFunc("POST /dev/enqueue", s.handleEnqueue)
	mux.HandleFunc("GET /dev/collectors", s.handleListCollectors)
	return mux
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var session *clientSession
	defer func() {
		if session != nil {
			s.mu.Lock()
			delete(s.clients, session.instanceID)
			s.mu.Unlock()
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		env, err := protocol.DecodeEnvelope(data)
		if err != nil {
			continue
		}
		switch env.Type {
		case protocol.TypeCollectorHello:
			session = &clientSession{
				instanceID: env.InstanceID,
				conn:       conn,
			}
			s.mu.Lock()
			s.clients[env.InstanceID] = session
			s.mu.Unlock()
			ack := protocol.Envelope{
				Type:      protocol.TypeCollectorHelloAck,
				OK:        true,
				SessionID: uuid.NewString(),
				Message:   "dev-host connected",
			}
			_ = writeEnvelope(conn, ack)
		case protocol.TypeCollectorManifest:
			id := env.InstanceID
			if session != nil {
				id = session.instanceID
			}
			tools := 0
			if env.Manifest != nil {
				tools = len(env.Manifest.Tools)
			}
			s.log.Printf("[dev-host] manifest from %s tools=%d", id, tools)
		case protocol.TypeInstanceStatus:
			// heartbeat
		case protocol.TypePullTasks:
			if session == nil {
				continue
			}
			limit := env.Limit
			if limit <= 0 {
				limit = 1
			}
			calls := s.dequeue(session, limit)
			_ = writeEnvelope(conn, protocol.Envelope{
				Type:  protocol.TypePullTasksAck,
				Calls: calls,
			})
		case protocol.TypeToolResult:
			if env.ToolResult != nil {
				s.log.Printf("[dev-host] result id=%s status=%s", env.ToolResult.ID, env.ToolResult.Status)
			}
		case protocol.TypePing:
			_ = writeEnvelope(conn, protocol.Envelope{Type: protocol.TypePong})
		}
	}
}

func (s *Server) dequeue(session *clientSession, limit int) []protocol.ToolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(session.queue) == 0 {
		return nil
	}
	if limit > len(session.queue) {
		limit = len(session.queue)
	}
	out := make([]protocol.ToolCall, limit)
	copy(out, session.queue[:limit])
	session.queue = session.queue[limit:]
	return out
}

func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		InstanceID string             `json:"instance_id"`
		Mode       string             `json:"mode"` // queue | push
		Call       protocol.ToolCall  `json:"call"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Call.Type == "" {
		req.Call.Type = protocol.TypeToolCall
	}
	if req.Call.ID == "" {
		req.Call.ID = "call_" + uuid.NewString()
	}

	s.mu.Lock()
	session, ok := s.clients[req.InstanceID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "collector not connected", http.StatusNotFound)
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "queue"
	}
	if mode == "push" {
		data, err := json.Marshal(req.Call)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := session.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"mode": "push", "call_id": req.Call.ID})
		return
	}

	s.mu.Lock()
	session.queue = append(session.queue, req.Call)
	queued := len(session.queue)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"mode": "queue", "call_id": req.Call.ID, "queued": queued})
}

func (s *Server) handleListCollectors(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	ids := make([]string, 0, len(s.clients))
	for id := range s.clients {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"collectors": ids})
}

func (s *Server) authenticated(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == s.Token {
		return true
	}
	return r.Header.Get("X-DigGate-Token") == s.Token
}

func writeEnvelope(conn *websocket.Conn, env protocol.Envelope) error {
	data, err := env.Encode()
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ListenAndServe starts the dev host HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.log.Printf("[dev-host] listening on %s ws=%s", addr, s.WSPath)
	return srv.ListenAndServe()
}
