// Package process supervises standalone inference-server subprocesses
// (whisper-server, llama-server): spawn, wait for the loopback port to
// accept connections, then watch for unexpected exit and restart with
// backoff (RFC.md#architecture--tech-stack decision note, Monitoring & Alerting).
package process

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"sync"
	"time"
)

const (
	readinessPollInterval = 200 * time.Millisecond
	readinessTimeout      = 15 * time.Second
	maxRestartAttempts    = 5
	baseRestartBackoff    = 500 * time.Millisecond
)

// Supervisor manages one long-lived subprocess bound to 127.0.0.1:Port.
type Supervisor struct {
	Name string
	Bin  string
	Args []string
	Port int

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
	failed  bool
}

func New(name, bin string, args []string, port int) *Supervisor {
	return &Supervisor{Name: name, Bin: bin, Args: args, Port: port}
}

// Start spawns the subprocess, blocks until its loopback port is accepting
// connections (or readinessTimeout elapses), and launches the watchdog
// goroutine that restarts it on unexpected exit.
func (s *Supervisor) Start(ctx context.Context) error {
	if err := s.spawn(); err != nil {
		return err
	}
	if err := s.waitReady(ctx); err != nil {
		s.killLocked()
		return err
	}
	go s.watch(ctx)
	return nil
}

// Stop terminates the subprocess and prevents the watchdog from restarting it.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.killLocked()
}

// Healthy reports whether the subprocess is running (not permanently failed
// after exhausting restart attempts).
func (s *Supervisor) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.failed
}

func (s *Supervisor) spawn() error {
	cmd := exec.Command(s.Bin, s.Args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %s (%s): %w", s.Name, s.Bin, err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) killLocked() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (s *Supervisor) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(readinessTimeout)
	addr := fmt.Sprintf("127.0.0.1:%d", s.Port)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, readinessPollInterval)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(readinessPollInterval)
	}
	return fmt.Errorf("%s did not become ready on %s within %s", s.Name, addr, readinessTimeout)
}

// watch waits for the subprocess to exit; if it wasn't a requested Stop, it
// restarts with exponential backoff up to maxRestartAttempts before marking
// the subsystem permanently failed (RFC.md#monitoring--alerting).
func (s *Supervisor) watch(ctx context.Context) {
	attempt := 0
	for {
		s.mu.Lock()
		cmd := s.cmd
		s.mu.Unlock()
		if cmd == nil {
			return
		}

		err := cmd.Wait()

		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}

		attempt++
		slog.Error("subprocess exited unexpectedly", "name", s.Name, "err", err, "attempt", attempt)

		if attempt > maxRestartAttempts {
			s.mu.Lock()
			s.failed = true
			s.mu.Unlock()
			slog.Error("subprocess exceeded restart attempts, giving up", "name", s.Name, "max_attempts", maxRestartAttempts)
			return
		}

		backoff := baseRestartBackoff * time.Duration(1<<uint(attempt-1))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		if err := s.spawn(); err != nil {
			slog.Error("subprocess restart failed", "name", s.Name, "err", err)
			continue
		}
		if err := s.waitReady(ctx); err != nil {
			slog.Error("subprocess restart did not become ready", "name", s.Name, "err", err)
		}
	}
}
