# Naira

Interactive offline-first AI companion robot for children. Full technical
design lives in [RFC.md](./RFC.md); product requirements in [PRD.md](./PRD.md).

## Privacy & Data Handling (read before setup)

- Naira listens continuously for its wake word only. No audio is recorded,
  saved, or transcribed until the wake word is heard.
- Once triggered, your child's speech is transcribed in-memory and used only
  to generate a response. The audio is discarded immediately after
  transcription — it is never written to disk and never leaves this device.
- Naira's core conversation (speech, understanding, response) runs fully
  offline, on this device. No voice data is ever sent to the internet.
- One optional skill (building simple games/apps on request) needs internet
  access and an authorized Claude CLI / OpenCode key. Naira will always ask
  out loud if it can't do this because of missing internet or authorization
  — it will not fail silently. Only the text of the request is sent, never
  audio.
- Local usage data (session times, skill usage, screen-time minutes,
  generated app names) is stored only in a local file on this device
  (`~/.naira/state.json`) and is never transmitted anywhere.

A parent/guardian must explicitly acknowledge this disclosure — via
`naira setup` — before the device leaves first-run setup mode. See
[RFC.md §3 Security Implications](./RFC.md#security-implications) and
[§4 Rollout Strategy](./RFC.md#rollout-strategy).

## Architecture

Clean architecture, ports-and-adapters style:

```
cmd/naira/            entrypoint
internal/domain/      entities + port interfaces (State, ModelsConfig, LLM tag contract, repository/engine ports) — no external deps
internal/usecase/     application logic against domain ports only (state, models, conversation)
internal/adapter/      port implementations (state.json repo, models.yaml repo via viper, HTTP downloader, connectivity checker, stub STT/LLM/TTS/Agent engines)
internal/cli/          cobra command tree (root, setup, models, run)
internal/config/       ~/.naira path resolution
```

Dependency direction: `cli` → `usecase` → `domain` ← `adapter`. The
`domain` package defines every interface (`StateRepository`, `LLMEngine`,
`AgentEngine`, etc.); `usecase` depends only on those interfaces, never on
`adapter` concrete types.

STT/LLM run as standalone `whisper-server`/`llama-server` subprocesses (not
CGo bindings — see [RFC.md decision note](./RFC.md#architecture--tech-stack)),
spawned and supervised by `internal/adapter/process`, called over loopback
HTTP by `internal/adapter/engine`'s `WhisperServerSTT`/`LlamaServerLLM`. Set
`server_bin`/`port`/`args` per subsystem in `~/.naira/models.yaml`; leaving
`server_bin` empty falls back to a stub so the rest of the orchestrator
stays exercisable without those binaries installed.

## Build & Run

```
go build -o naira ./cmd/naira

./naira setup            # first-run parent consent gate (required once)
./naira models list       # show configured vs. present-on-disk model files
./naira models download   # fetch models per ~/.naira/models.yaml (needs url+sha256 filled in)
./naira run                # start the orchestrator loop
```

All state lives under `~/.naira/` (override with `--home` or `NAIRA_HOME`):
`state.json` (runtime state), `models.yaml` (STT/LLM/TTS model config),
`models/` (downloaded model files), `games/` (EXECUTE_AGENT output), `logs/`.

**Current status:** STT/LLM/wake-word/TTS are wired against real
`whisper-server`/`llama-server`/`openwakeword_server.py`/`piper` subprocesses
when the corresponding `server_bin` is set in `models.yaml` (each falls back
to a stub otherwise). The Claude CLI / OpenCode agent sandbox is still
stubbed (see [RFC.md §5 Concerns](./RFC.md#5-concerns-questions-or-known-limitations)).
`naira run --audio` drives real microphone capture through wake-word-gated
VAD/endpointing/STT/LLM/TTS end-to-end when all four are configured;
default (no `--audio`) mode reads plain-text lines from stdin in place of
microphone input, still useful for exercising the orchestrator, state
persistence, and tag-routing/gating logic without any binaries installed.
