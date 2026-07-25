// Package config resolves on-disk locations for Naira's runtime files.
package config

import (
	"os"
	"path/filepath"
)

// Home is ~/.naira — the root for state.json, models.yaml, logs, and models/.
func Home() (string, error) {
	if v := os.Getenv("NAIRA_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".naira"), nil
}

// EnsureHome resolves Home and makes sure the directory exists.
func EnsureHome() (string, error) {
	dir, err := Home()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func StatePath(home string) string      { return filepath.Join(home, "state.json") }
func ModelsYAMLPath(home string) string { return filepath.Join(home, "models.yaml") }
func LogsDir(home string) string        { return filepath.Join(home, "logs") }
func ModelsDir(home string) string      { return filepath.Join(home, "models") }
func GamesDir(home string) string       { return filepath.Join(home, "games") }
