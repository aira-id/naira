// Package downloader implements domain.Downloader over HTTP with sha256
// verification (RFC.md#configuration, Security Implications: model integrity).
package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type HTTP struct {
	Client *http.Client
}

func NewHTTP() *HTTP {
	return &HTTP{Client: http.DefaultClient}
}

// Download fetches url into destPath. The response body is streamed to a
// temp file while hashing; on checksum mismatch the temp file is discarded
// and destPath is left untouched, so a corrupted/tampered download can never
// silently become the active model file.
func (d *HTTP) Download(ctx context.Context, url, destPath, wantSHA256 string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: unexpected status %s", url, resp.Status)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(destPath)+".download-*")
	if err != nil {
		return fmt.Errorf("create temp download file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed into place

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if wantSHA256 != "" && sum != wantSHA256 {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, sum, wantSHA256)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename into place %s: %w", destPath, err)
	}
	return nil
}
