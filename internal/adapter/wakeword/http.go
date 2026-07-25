package wakeword

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// detectTimeout bounds each per-frame round trip — Detect is called on every
// 20ms audio frame while idle-listening (RFC.md#sequence), so a hung request
// must not stall the capture loop indefinitely.
const detectTimeout = 200 * time.Millisecond

// HTTPDetector implements domain.WakeWordDetector against a standalone
// openwakeword_server.py subprocess (scripts/openwakeword_server.py),
// reachable on loopback only — same subprocess+HTTP pattern as
// WhisperServerSTT/LlamaServerLLM (RFC.md#architecture--tech-stack decision
// note). A single persistent Model instance on the server side carries
// streaming state across calls, so frames must be submitted in order by one
// caller only.
type HTTPDetector struct {
	BaseURL string // e.g. http://127.0.0.1:8082
	Client  *http.Client
}

func NewHTTPDetector(baseURL string) *HTTPDetector {
	return &HTTPDetector{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: detectTimeout},
	}
}

// Detect posts one raw PCM16 frame to the server and reports whether the
// wake phrase's score crossed threshold. Fails safe: any transport/decode
// error is logged and treated as "not detected" rather than propagated,
// since domain.WakeWordDetector.Detect has no error return and a missed
// wake trigger is far less harmful than crashing the capture loop.
func (d *HTTPDetector) Detect(frame []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.BaseURL+"/detect", bytes.NewReader(frame))
	if err != nil {
		slog.Warn("wakeword: build request failed", "err", err)
		return false
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		slog.Warn("wakeword: openwakeword_server request failed", "err", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("wakeword: openwakeword_server returned non-200", "status", resp.StatusCode)
		return false
	}

	var out struct {
		Detected bool    `json:"detected"`
		Score    float64 `json:"score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.Warn("wakeword: decode openwakeword_server response failed", "err", err)
		return false
	}
	return out.Detected
}
