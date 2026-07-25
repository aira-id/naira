// Package state implements the application logic sitting on top of
// domain.StateRepository: load/prune at startup, mutex-guarded in-memory
// mutation, and save-on-mutation persistence (RFC.md#local-state-storage).
package state

import (
	"context"
	"fmt"
	"sync"
	"time"

	"naira/internal/domain"
	"naira/internal/idgen"
)

// RetentionWindow is how long append-only logs (sessions, screen_time_log,
// skill_usage) are kept before being pruned on load, so state.json doesn't
// grow unbounded on a long-running device (RFC.md#local-state-storage).
const RetentionWindow = 90 * 24 * time.Hour

// DisclosureVersion is the current parent privacy/data-handling disclosure
// version. Bump when the disclosure text materially changes so previously
// consenting parents are re-prompted.
const DisclosureVersion = "1.0"

// Service owns the in-memory State and persists it through repo on every
// mutation. Safe for concurrent use.
type Service struct {
	repo domain.StateRepository

	mu    sync.Mutex
	state *domain.State
}

func New(repo domain.StateRepository) *Service {
	return &Service{repo: repo}
}

// Load reads state.json (creating a fresh in-memory default if it doesn't
// exist yet), prunes expired log entries, and persists the pruned result.
func (s *Service) Load(ctx context.Context) error {
	st, err := s.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	s.mu.Lock()
	s.state = st
	pruned := s.pruneLocked()
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	if pruned {
		if err := s.repo.Save(ctx, snapshot); err != nil {
			return fmt.Errorf("save pruned state: %w", err)
		}
	}
	return nil
}

// Snapshot returns a copy of the current state for read-only inspection
// (e.g. CLI status output).
func (s *Service) Snapshot() domain.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.cloneLocked()
}

// cloneLocked deep-copies slice fields using make+copy rather than
// append(nil, x...), which collapses zero-length inputs to a nil slice —
// that would marshal as JSON null instead of [], breaking the state.json
// schema's array typing for a parent inspecting the file (RFC.md#local-state-storage).
func (s *Service) cloneLocked() *domain.State {
	c := *s.state

	c.Sessions = make([]domain.Session, len(s.state.Sessions))
	copy(c.Sessions, s.state.Sessions)

	c.ScreenTimeLog = make([]domain.ScreenTimeLog, len(s.state.ScreenTimeLog))
	copy(c.ScreenTimeLog, s.state.ScreenTimeLog)

	c.SkillUsage = make([]domain.SkillUsage, len(s.state.SkillUsage))
	copy(c.SkillUsage, s.state.SkillUsage)

	c.GeneratedApps = make([]domain.GeneratedApp, len(s.state.GeneratedApps))
	copy(c.GeneratedApps, s.state.GeneratedApps)

	return &c
}

// pruneLocked drops log entries older than RetentionWindow. Caller must
// hold s.mu. Returns true if anything was removed.
func (s *Service) pruneLocked() bool {
	cutoff := time.Now().UTC().Add(-RetentionWindow)
	changed := false

	sessions := make([]domain.Session, 0, len(s.state.Sessions))
	for _, sess := range s.state.Sessions {
		if t, err := time.Parse(time.RFC3339, sess.StartedAt); err == nil && t.Before(cutoff) {
			changed = true
			continue
		}
		sessions = append(sessions, sess)
	}
	s.state.Sessions = sessions

	logs := make([]domain.ScreenTimeLog, 0, len(s.state.ScreenTimeLog))
	for _, l := range s.state.ScreenTimeLog {
		if t, err := time.Parse("2006-01-02", l.Date); err == nil && t.Before(cutoff) {
			changed = true
			continue
		}
		logs = append(logs, l)
	}
	s.state.ScreenTimeLog = logs

	usage := make([]domain.SkillUsage, 0, len(s.state.SkillUsage))
	for _, u := range s.state.SkillUsage {
		if t, err := time.Parse(time.RFC3339, u.InvokedAt); err == nil && t.Before(cutoff) {
			changed = true
			continue
		}
		usage = append(usage, u)
	}
	s.state.SkillUsage = usage

	return changed
}

// IsConsented reports whether the parent consent gate has been satisfied.
func (s *Service) IsConsented() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.ParentConsent.Accepted()
}

// AcceptConsent records parent acknowledgment of the privacy/data-handling
// disclosure. This is a hard blocker for setup completion (RFC.md#rollout-strategy).
func (s *Service) AcceptConsent(ctx context.Context, deviceID string) error {
	s.mu.Lock()
	s.state.ParentConsent = domain.ParentConsent{
		DisclosureVersion: DisclosureVersion,
		AcceptedAt:        time.Now().UTC().Format(time.RFC3339),
		DeviceID:          deviceID,
	}
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	return s.repo.Save(ctx, snapshot)
}

// StartSession begins a new interaction window and returns its ID.
func (s *Service) StartSession(ctx context.Context) (string, error) {
	id := idgen.New()

	s.mu.Lock()
	s.state.Sessions = append(s.state.Sessions, domain.Session{
		ID:        id,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	})
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	return id, s.repo.Save(ctx, snapshot)
}

// EndSession stamps EndedAt and WakeCount on an existing session.
func (s *Service) EndSession(ctx context.Context, sessionID string, wakeCount int) error {
	s.mu.Lock()
	found := false
	for i := range s.state.Sessions {
		if s.state.Sessions[i].ID == sessionID {
			s.state.Sessions[i].EndedAt = time.Now().UTC().Format(time.RFC3339)
			s.state.Sessions[i].WakeCount = wakeCount
			found = true
			break
		}
	}
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	if !found {
		return fmt.Errorf("end session: unknown session id %q", sessionID)
	}
	return s.repo.Save(ctx, snapshot)
}

// RecordSkillUsage appends a local-only usage analytics entry.
func (s *Service) RecordSkillUsage(ctx context.Context, sessionID, skillName string) error {
	s.mu.Lock()
	s.state.SkillUsage = append(s.state.SkillUsage, domain.SkillUsage{
		SessionID: sessionID,
		SkillName: skillName,
		InvokedAt: time.Now().UTC().Format(time.RFC3339),
	})
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	return s.repo.Save(ctx, snapshot)
}

// LogScreenTime appends a per-day active-minutes entry, powering the
// [SLEEPY] screen-time-limit behavior (PRD §4.3 Skill 5).
func (s *Service) LogScreenTime(ctx context.Context, sessionID, date string, activeMinutes int) error {
	s.mu.Lock()
	s.state.ScreenTimeLog = append(s.state.ScreenTimeLog, domain.ScreenTimeLog{
		SessionID:     sessionID,
		Date:          date,
		ActiveMinutes: activeMinutes,
	})
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	return s.repo.Save(ctx, snapshot)
}

// ScreenTimeExceeded sums today's active minutes against the configured
// threshold (RuntimeConfig.ScreenTimeThresholdMinutes).
func (s *Service) ScreenTimeExceeded(date string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := 0
	for _, l := range s.state.ScreenTimeLog {
		if l.Date == date {
			total += l.ActiveMinutes
		}
	}
	return total >= s.state.Config.ScreenTimeThresholdMinutes
}

// RegisterGeneratedApp records a newly built app/game so the robot can
// reopen it later without regenerating (RFC.md#local-state-storage).
func (s *Service) RegisterGeneratedApp(ctx context.Context, name, appType, fsPath, promptText string) (domain.GeneratedApp, error) {
	app := domain.GeneratedApp{
		ID:         idgen.New(),
		Name:       name,
		AppType:    appType,
		FSPath:     fsPath,
		PromptText: promptText,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	s.mu.Lock()
	s.state.GeneratedApps = append(s.state.GeneratedApps, app)
	snapshot := s.cloneLocked()
	s.mu.Unlock()

	return app, s.repo.Save(ctx, snapshot)
}

// FindGeneratedApp looks up a previously generated app by name.
func (s *Service) FindGeneratedApp(name string) (domain.GeneratedApp, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.state.GeneratedApps {
		if a.Name == name {
			return a, true
		}
	}
	return domain.GeneratedApp{}, false
}

// Config returns the current runtime config (wake word, TTS voice, etc).
func (s *Service) Config() domain.RuntimeConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Config
}
