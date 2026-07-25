// Package repository implements domain repository ports.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"naira/internal/domain"
)

// StateJSON implements domain.StateRepository as a single flat JSON file,
// written via the write-temp/fsync/atomic-rename pattern so a crash mid-write
// leaves the last-good file intact (RFC.md#local-state-storage).
type StateJSON struct {
	path string
}

func NewStateJSON(path string) *StateJSON {
	return &StateJSON{path: path}
}

func (r *StateJSON) Load(ctx context.Context) (*domain.State, error) {
	b, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.NewState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", r.path, err)
	}

	var st domain.State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", r.path, err)
	}

	if st.SchemaVersion < domain.CurrentSchemaVersion {
		migrate(&st)
	}
	return &st, nil
}

// migrate applies in-place, in-memory migrations when an on-disk
// schema_version is older than domain.CurrentSchemaVersion
// (RFC.md#local-state-storage). No migrations exist yet at v1.
func migrate(st *domain.State) {
	st.SchemaVersion = domain.CurrentSchemaVersion
}

func (r *StateJSON) Save(ctx context.Context, s *domain.State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(r.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("fsync temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmpPath, r.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp state file into place: %w", err)
	}
	return nil
}
