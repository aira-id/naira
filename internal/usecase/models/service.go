// Package models implements `naira models list` / `naira models download`
// business logic on top of domain ports (RFC.md#configuration).
package models

import (
	"context"
	"fmt"
	"os"

	"naira/internal/domain"
)

type Service struct {
	configRepo   domain.ModelsConfigRepository
	connectivity domain.ConnectivityChecker
	downloader   domain.Downloader
}

func New(configRepo domain.ModelsConfigRepository, connectivity domain.ConnectivityChecker, downloader domain.Downloader) *Service {
	return &Service{configRepo: configRepo, connectivity: connectivity, downloader: downloader}
}

// Status is one row of `naira models list` output.
type Status struct {
	Subsystem domain.Subsystem
	Name      string
	Path      string
	Present   bool
	Fetchable bool
}

// List reports configured vs. present-on-disk status per subsystem, no
// download side effects (RFC.md#apis Model Management CLI).
func (s *Service) List(ctx context.Context) ([]Status, error) {
	cfg, err := s.configRepo.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load models.yaml: %w", err)
	}

	var out []Status
	for _, e := range cfg.Entries() {
		_, err := os.Stat(e.Entry.Path)
		out = append(out, Status{
			Subsystem: e.Subsystem,
			Name:      e.Entry.Name(),
			Path:      e.Entry.Path,
			Present:   err == nil,
			Fetchable: e.Entry.Fetchable(),
		})
	}
	return out, nil
}

// DownloadOptions controls `naira models download` scope.
type DownloadOptions struct {
	Only  domain.Subsystem // empty = all
	Force bool
}

// Result is the outcome for one model entry download attempt.
type Result struct {
	Subsystem domain.Subsystem
	Name      string
	Skipped   bool
	Message   string
	Err       error
}

// Download fetches missing/mismatched models per models.yaml, verifying
// sha256 before activation. Mirrors the sequence in
// RFC.md#model-download-subcommand: presence+checksum check, connectivity
// check, fetch, checksum verify, write.
func (s *Service) Download(ctx context.Context, opts DownloadOptions) ([]Result, error) {
	cfg, err := s.configRepo.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load models.yaml: %w", err)
	}

	var results []Result
	for _, e := range cfg.Entries() {
		if opts.Only != "" && e.Subsystem != opts.Only {
			continue
		}
		results = append(results, s.downloadOne(ctx, e.Subsystem, e.Entry, opts.Force))
	}
	return results, nil
}

func (s *Service) downloadOne(ctx context.Context, sub domain.Subsystem, entry domain.ModelEntry, force bool) Result {
	res := Result{Subsystem: sub, Name: entry.Name()}

	if !force {
		if _, err := os.Stat(entry.Path); err == nil {
			res.Skipped = true
			res.Message = "already present"
			return res
		}
	}

	if !entry.Fetchable() {
		res.Err = fmt.Errorf("no url/sha256 configured — place the file manually at %s", entry.Path)
		return res
	}

	if !s.connectivity.Online(ctx) {
		res.Err = fmt.Errorf("no internet — place the file manually at %s, or retry later", entry.Path)
		return res
	}

	if err := s.downloader.Download(ctx, entry.URL, entry.Path, entry.SHA256); err != nil {
		res.Err = fmt.Errorf("download failed: %w", err)
		return res
	}

	res.Message = "downloaded and verified"
	return res
}
