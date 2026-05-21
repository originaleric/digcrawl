package host

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/originaleric/diggate/protocol"
)

// Server is a reusable host-side Collector WebSocket endpoint.
type Server struct {
	Token  string
	WSPath string
	Log    *log.Logger

	mu       sync.Mutex
	sessions map[string]*Session
}

// Session is one connected collector instance.
type Session struct {
	InstanceID  string
	Manifest    *protocol.ToolManifest
	Status      string
	Active      int
	ConnectedAt time.Time

	conn    *websocket.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	queue   []protocol.ToolCall
}

// NewServer creates a host-side collector server.
func NewServer(token, wsPath string) *Server {
	if wsPath == "" {
		wsPath = "/diggate/v1/collector/ws"
	}
	return &Server{Token: token, WSPath: wsPath, Log: log.Default(), sessions: map[string]*Session{}}
}

// Handler exposes health and collector WebSocket endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc(s.WSPath, s.handleWS)
	return mux
}

// Enqueue queues a call for pull-mode collectors.
func (s *Server) Enqueue(instanceID string, call protocol.ToolCall) error {
	session, ok := s.session(instanceID)
	if !ok {
		return fmt.Errorf("collector %q not connected", instanceID)
	}
	normalizeCall(&call)
	session.mu.Lock()
	session.queue = append(session.queue, call)
	session.mu.Unlock()
	return nil
}

// Push immediately sends a call to a connected collector.
func (s *Server) Push(instanceID string, call protocol.ToolCall) error {
	session, ok := s.session(instanceID)
	if !ok {
		return fmt.Errorf("collector %q not connected", instanceID)
	}
	normalizeCall(&call)
	return session.writeEnvelope(protocol.Envelope{Type: protocol.TypeToolCall, ToolCall: &call})
}

// Collectors returns connected instance IDs.
func (s *Server) Collectors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (s *Server) session(instanceID string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[instanceID]
	return session, ok
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Upgrade(w, r, nil, 1<<20, 1<<20)
	if err != nil {
		return
	}
	defer conn.Close()

	var session *Session
	defer func() {
		if session != nil {
			s.mu.Lock()
			delete(s.sessions, session.InstanceID)
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
			_ = writeEnvelope(conn, protocol.NewWireError("INVALID_ENVELOPE", err.Error()))
			continue
		}
		switch env.Type {
		case protocol.TypeCollectorHello:
			session = &Session{InstanceID: env.InstanceID, Status: "online", ConnectedAt: time.Now(), conn: conn}
			s.mu.Lock()
			s.sessions[env.InstanceID] = session
			s.mu.Unlock()
			_ = session.writeEnvelope(protocol.Envelope{Type: protocol.TypeCollectorHelloAck, OK: true, SessionID: uuid.NewString()})
		case protocol.TypeCollectorManifest:
			if session != nil {
				session.Manifest = env.Manifest
			}
		case protocol.TypeInstanceStatus:
			if session != nil {
				session.Status = env.Status
				session.Active = env.ActiveCalls
			}
		case protocol.TypePullTasks:
			if session != nil {
				_ = session.writeEnvelope(protocol.Envelope{Type: protocol.TypePullTasksAck, Calls: session.dequeue(env.Limit)})
			}
		case protocol.TypeToolResult:
			if env.ToolResult != nil && s.Log != nil {
				s.Log.Printf("[host] result id=%s status=%s", env.ToolResult.ID, env.ToolResult.Status)
			}
		case protocol.TypePing:
			if session != nil {
				_ = session.writeEnvelope(protocol.Envelope{Type: protocol.TypePong})
			} else {
				_ = writeEnvelope(conn, protocol.Envelope{Type: protocol.TypePong})
			}
		}
	}
}

func (s *Session) dequeue(limit int) []protocol.ToolCall {
	if limit <= 0 {
		limit = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil
	}
	if limit > len(s.queue) {
		limit = len(s.queue)
	}
	out := make([]protocol.ToolCall, limit)
	copy(out, s.queue[:limit])
	s.queue = s.queue[limit:]
	return out
}

func (s *Session) writeEnvelope(env protocol.Envelope) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeEnvelope(s.conn, env)
}

func (s *Server) authenticated(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.Token {
		return true
	}
	return r.Header.Get("X-DigGate-Token") == s.Token
}

func normalizeCall(call *protocol.ToolCall) {
	if call.Type == "" {
		call.Type = protocol.TypeToolCall
	}
	if call.ID == "" {
		call.ID = "call_" + uuid.NewString()
	}
}

func writeEnvelope(conn *websocket.Conn, env protocol.Envelope) error {
	data, err := json.Marshal(env)
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

// ListenAndServe starts the host endpoint.
func (s *Server) ListenAndServe(addr string) error {
	return (&http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe()
}
