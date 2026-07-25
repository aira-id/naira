package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"naira/internal/domain"
)

// tagPrefixPattern matches the leading "[EXPRESSION_TAG] [ACTION_TAG] " the
// LLM emits before its spoken text (RFC.md#apis). Streamed sentences must
// never include this prefix — it's a control signal, not something to speak.
var tagPrefixPattern = regexp.MustCompile(`^\[[A-Z_]+\]\s*\[[A-Z_]+\]\s*`)

// LlamaServerLLM implements domain.LLMEngine against a standalone
// llama-server subprocess (llama.cpp's HTTP server), reachable on loopback
// only. It streams the completion and invokes onSentence as each sentence
// completes, so TTS can start before the full response is generated
// (RFC.md#sequence).
type LlamaServerLLM struct {
	BaseURL string // e.g. http://127.0.0.1:8080
	Client  *http.Client
}

func NewLlamaServerLLM(baseURL string) *LlamaServerLLM {
	return &LlamaServerLLM{BaseURL: baseURL, Client: http.DefaultClient}
}

type llamaCompletionRequest struct {
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type llamaStreamChunk struct {
	Content string `json:"content"`
	Stop    bool   `json:"stop"`
}

func (l *LlamaServerLLM) Infer(ctx context.Context, prompt string, onSentence func(sentence string)) (domain.LLMOutput, error) {
	reqBody, err := json.Marshal(llamaCompletionRequest{Prompt: prompt, Stream: true})
	if err != nil {
		return domain.LLMOutput{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.BaseURL+"/completion", bytes.NewReader(reqBody))
	if err != nil {
		return domain.LLMOutput{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.Client.Do(req)
	if err != nil {
		return domain.LLMOutput{}, fmt.Errorf("call llama-server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.LLMOutput{}, fmt.Errorf("llama-server returned %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var full strings.Builder    // raw stream, tags included — fed to ParseLLMOutput at the end
	var pending strings.Builder // raw content buffered until the tag prefix is fully seen
	var sentence strings.Builder
	tagsStripped := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "data: ")
		if line == "" {
			continue
		}

		var chunk llamaStreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue // skip malformed/keepalive lines rather than aborting the stream
		}
		full.WriteString(chunk.Content)

		var textChunk string
		if !tagsStripped {
			pending.WriteString(chunk.Content)
			if loc := tagPrefixPattern.FindStringIndex(pending.String()); loc != nil {
				tagsStripped = true
				textChunk = pending.String()[loc[1]:]
			} else if chunk.Stop {
				// Stream ended before the tag prefix ever completed — nothing
				// speakable was ever resolved; fall through to break below.
			} else {
				continue
			}
		} else {
			textChunk = chunk.Content
		}

		sentence.WriteString(textChunk)
		if strings.ContainsAny(textChunk, ".!?\n") {
			if onSentence != nil {
				if s := strings.TrimSpace(sentence.String()); s != "" {
					onSentence(s)
				}
			}
			sentence.Reset()
		}

		if chunk.Stop {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return domain.LLMOutput{}, fmt.Errorf("read llama-server stream: %w", err)
	}

	if onSentence != nil {
		if s := strings.TrimSpace(sentence.String()); s != "" {
			onSentence(s)
		}
	}

	return domain.ParseLLMOutput(full.String())
}
