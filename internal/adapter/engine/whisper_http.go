package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// WhisperServerSTT implements domain.STTEngine against a standalone
// whisper-server subprocess (whisper.cpp's HTTP server example), reachable
// on loopback only. The audio buffer is held in memory and sent as a
// multipart upload — never written to disk (RFC.md Security Implications).
type WhisperServerSTT struct {
	BaseURL string // e.g. http://127.0.0.1:8081
	Client  *http.Client
}

func NewWhisperServerSTT(baseURL string) *WhisperServerSTT {
	return &WhisperServerSTT{BaseURL: baseURL, Client: http.DefaultClient}
}

func (w *WhisperServerSTT) Transcribe(ctx context.Context, pcm []byte) (string, error) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)

	part, err := mw.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("build multipart request: %w", err)
	}
	if _, err := part.Write(pcm); err != nil {
		return "", fmt.Errorf("write audio buffer to request: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.BaseURL+"/inference", body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := w.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call whisper-server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("whisper-server returned %s: %s", resp.Status, string(b))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode whisper-server response: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}
