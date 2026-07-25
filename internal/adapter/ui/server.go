// Package ui implements domain.UIPublisher via a loopback-only HTTP+
// WebSocket server (RFC.md#apis Internal IPC, architecture note: "Go webview
// / Neutralinojs / HTML5 Canvas"). The Go orchestrator serves a static
// face-animation client (internal/adapter/ui/static) and broadcasts the
// state_change/mouth_amplitude/window_mode/agent_status messages over
// WebSocket to whatever renders it — a kiosk-mode browser window today,
// swappable for a native webview/Neutralinojs shell later without changing
// this port's wire format.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"naira/internal/domain"
)

//go:embed static
var staticFiles embed.FS

var upgrader = websocket.Upgrader{
	// Loopback-only server (see Server.addr) — any local process can already
	// reach it, so origin checking adds no real boundary here.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server implements domain.UIPublisher by broadcasting IPC messages to every
// connected WebSocket client. Bound to 127.0.0.1 only, matching the
// loopback-only posture of whisper-server/llama-server/openwakeword_server
// (RFC.md Security Implications).
type Server struct {
	addr string

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}

	// lastState/lastMode cache the most recent state_change/window_mode
	// frame so a newly connected (or reconnected) client is brought current
	// immediately instead of waiting for the next state transition — without
	// this, a browser reload would get stuck on the initial splash frame
	// until the orchestrator happened to change expression again.
	lastState []byte
	lastMode  []byte

	httpServer *http.Server
}

// NewServer builds a Server bound to 127.0.0.1:port. Call Start to begin
// listening.
func NewServer(port int) *Server {
	return &Server{
		addr:    fmt.Sprintf("127.0.0.1:%d", port),
		clients: make(map[*websocket.Conn]struct{}),
	}
}

// Start begins serving the static face client and WebSocket endpoint,
// returning once the listener is up. Shutdown happens via ctx cancellation.
func (s *Server) Start(ctx context.Context) error {
	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("ui: load embedded static assets: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(staticRoot)))
	mux.HandleFunc("/ws", s.handleWS)

	s.httpServer = &http.Server{Addr: s.addr, Handler: mux}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("ui: listen on %s: %w", s.addr, err)
	}

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("ui server exited unexpectedly", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()

	return nil
}

// URL is the address a browser/webview should point at.
func (s *Server) URL() string {
	return "http://" + s.addr
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ui: websocket upgrade failed", "err", err)
		return
	}

	s.mu.Lock()
	s.clients[conn] = struct{}{}
	lastState, lastMode := s.lastState, s.lastMode
	s.mu.Unlock()

	for _, cached := range [][]byte{lastState, lastMode} {
		if cached == nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, cached); err != nil {
			slog.Warn("ui: replay cached state to new client failed", "err", err)
			return
		}
	}

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	// Drain and discard any client->server frames (client is display-only);
	// this also detects disconnects so the client set doesn't grow stale.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// broadcast sends msg to every connected client. If cacheSlot is non-nil, it
// points at the Server field (lastState or lastMode) that should remember
// this frame for replay to future connections — see handleWS.
func (s *Server) broadcast(msg any, cacheSlot *[]byte) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("ui: marshal message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cacheSlot != nil {
		*cacheSlot = body
	}
	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
			slog.Warn("ui: broadcast to client failed, dropping", "err", err)
			conn.Close()
			delete(s.clients, conn)
		}
	}
	return nil
}

// SetState implements domain.UIPublisher (state_change, RFC.md#apis).
func (s *Server) SetState(ctx context.Context, state domain.ExpressionTag) error {
	return s.broadcast(struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}{"state_change", string(state)}, &s.lastState)
}

// SetWindowMode implements domain.UIPublisher (window_mode, RFC.md#apis).
func (s *Server) SetWindowMode(ctx context.Context, floating bool, w, h int) error {
	mode := "FULLSCREEN"
	if floating {
		mode = "FLOATING"
	}
	return s.broadcast(struct {
		Type string `json:"type"`
		Mode string `json:"mode"`
		W    int    `json:"w"`
		H    int    `json:"h"`
	}{"window_mode", mode, w, h}, &s.lastMode)
}

// MouthAmplitude implements domain.UIPublisher (mouth_amplitude, RFC.md#apis).
func (s *Server) MouthAmplitude(ctx context.Context, amplitude float64, tsMillis int64) error {
	return s.broadcast(struct {
		Type      string  `json:"type"`
		Amplitude float64 `json:"amplitude"`
		TS        int64   `json:"ts"`
	}{"mouth_amplitude", amplitude, tsMillis}, nil)
}

// AgentStatus implements domain.UIPublisher (agent_status, RFC.md#apis).
func (s *Server) AgentStatus(ctx context.Context, status, jobID string) error {
	return s.broadcast(struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		JobID  string `json:"job_id"`
	}{"agent_status", status, jobID}, nil)
}
