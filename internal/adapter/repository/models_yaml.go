package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"

	"naira/internal/domain"
)

// defaultModelsYAML mirrors the schema documented in RFC.md#configuration.
// sha256 fields are left blank — a parent/operator must fill them in (or
// accept the manual-copy fallback) before `naira models download` will
// auto-fetch, since Fetchable() requires both url and sha256.
const defaultModelsYAML = `stt:
  engine: whisper.cpp
  model: base          # base | small
  quant: int8
  path: ./models/ggml-base-int8.bin
  url: https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base-int8.bin
  sha256: ""
  server_bin: ""       # path to the whisper-server binary (empty = STT disabled)
  port: 8081
  args: []

llm:
  engine: llama.cpp
  model: Qwen2.5-1.5B-Instruct
  quant: Q4_K_M
  path: ./models/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf
  url: https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf
  sha256: ""
  server_bin: ""       # path to the llama-server binary (empty = LLM disabled)
  port: 8080
  args: ["-t", "2", "-c", "1024", "--mlock"]

tts:
  engine: piper
  voice: id_ID-news_tts-medium
  path: ./models/id_ID-news_tts-medium.onnx
  config_path: ./models/id_ID-news_tts-medium.onnx.json
  url: https://huggingface.co/rhasspy/piper-voices/resolve/main/id/id_ID/news_tts/medium/id_ID-news_tts-medium.onnx
  sha256: ""

wakeword:
  engine: openwakeword
  model: hey_jarvis_v0.1     # stock pretrained phrase (RFC.md §5: no custom "hey naira" model trained yet)
  path: ./models/openwakeword   # cache dir; openwakeword's own downloader fetches melspectrogram/embedding/wakeword onnx files here on first run
  url: ""                     # not fetched via naira models download — openwakeword manages its own model cache
  sha256: ""
  server_bin: ""              # path to a python3 interpreter (empty = wake-word disabled, falls back to NoOp stub)
  port: 8082
  args: ["scripts/openwakeword_server.py", "--model", "hey_jarvis_v0.1", "--cache-dir", "./models/openwakeword", "--threshold", "0.5"]
`

// EnsureDefault writes the default models.yaml template to path if nothing
// exists there yet. It never overwrites an existing file.
func EnsureDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(defaultModelsYAML), 0o644); err != nil {
		return fmt.Errorf("write default %s: %w", path, err)
	}
	return nil
}

// ModelsYAML implements domain.ModelsConfigRepository by parsing models.yaml
// via viper (RFC.md#configuration).
type ModelsYAML struct {
	path string
}

func NewModelsYAML(path string) *ModelsYAML {
	return &ModelsYAML{path: path}
}

func (r *ModelsYAML) Load(ctx context.Context) (*domain.ModelsConfig, error) {
	v := viper.New()
	v.SetConfigFile(r.path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read %s: %w", r.path, err)
	}

	var cfg domain.ModelsConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", r.path, err)
	}
	return &cfg, nil
}
