# Naira — Implementation Status & TODO

Tracks what's been built vs. what's still open. See [RFC.md](./RFC.md) for
full design rationale and [PRD.md](./PRD.md) for product requirements.

## Done

### Architecture & scaffolding
- Clean architecture: `domain` (entities + ports) ← `adapter` (port implementations), `usecase` (business logic on ports only) ← `cli` (cobra command tree). Dependency direction enforced: `cli` → `usecase` → `domain` ← `adapter`.
- `cobra` for command tree, `viper` for config parsing (`models.yaml`).
- `go build`/`go vet`/`gofmt` all clean across the tree.

### State & configuration
- `internal/domain/state.go` — `State` schema (`schema_version`, `parent_consent`, `config`, `sessions`, `screen_time_log`, `skill_usage`, `generated_apps`), matches RFC's `state.json` schema exactly.
- `internal/adapter/repository/state_json.go` — flat-file JSON persistence, write-temp/fsync/atomic-rename pattern for crash safety, in-memory schema-version migration hook.
- `internal/usecase/state` — mutex-guarded state service: parent consent gate, session/skill-usage/screen-time logging, 90-day log pruning on load, screen-time threshold check.
- `internal/domain/models.go` + `internal/adapter/repository/models_yaml.go` — `models.yaml` schema (stt/llm/tts: engine, model, path, url, sha256, `server_bin`/`port`/`args`), viper-parsed, default template auto-written on first run (checksums blank — must be filled in).
- `internal/usecase/models` + `internal/adapter/downloader` — `naira models list`/`naira models download [--only] [--force]`: connectivity-gated, sha256-verified before activation.

### Core conversation pipeline
- `internal/domain/conversation.go` — `[EXPRESSION_TAG][ACTION_TAG] text` parsing/validation contract.
- `internal/usecase/conversation` — tag-router dispatch; `EXECUTE_AGENT`/`OPEN_BROWSER` gated on connectivity + auth with vocal fallback messaging; sandboxed app-name sanitization.
- **STT/LLM run as standalone subprocesses, not CGo** (decision recorded in RFC.md#architecture--tech-stack): `internal/adapter/process` supervises `whisper-server`/`llama-server` (spawn, loopback-port readiness poll, crash auto-restart with backoff, permanent-fail after 5 attempts). `internal/adapter/engine` calls them over loopback HTTP (`WhisperServerSTT` multipart upload, `LlamaServerLLM` streaming completion with proper tag-prefix stripping before anything reaches TTS).
- **Audio capture, VAD, endpointing** (no CGo): `internal/adapter/audio` (arecord/parec subprocess → fixed 20ms PCM16 frames + in-memory WAV encoding), `internal/adapter/vad` (RMS-threshold + adaptive-noise-floor energy VAD), `internal/usecase/listening` (wake-word-gated capture, ~300ms pre-roll seed, 700ms silence cutoff, 20s max-utterance safety cap).
- `internal/adapter/wakeword` — `NoOp` stub (never fires, used when `wakeword.server_bin` unset) and `Always` (dev/test-only, never wired into the shipped CLI). **Real engine wired**: `HTTPDetector` calls `scripts/openwakeword_server.py` (openWakeWord, stock pretrained phrase `hey_jarvis_v0.1`), a standalone subprocess supervised the same way as `whisper-server`/`llama-server` — see RFC.md §5 Concerns. No custom "Hey Naira" model trained yet (would require openWakeWord's own training pipeline); default `wake_word` config value is `"hey jarvis"` to match.
- **Piper TTS wired, CLI-per-utterance subprocess (not CGo)**: `internal/adapter/tts.PiperCLI` spawns `piper` fresh per sentence (Piper ships no server mode, unlike whisper.cpp/llama.cpp — decision recorded in RFC.md#architecture--tech-stack) piping `--output-raw` PCM directly into a playback subprocess (`--tts-player-bin`, default `aplay`) without buffering the whole utterance, preserving sentence-level streaming latency. Sample rate read from Piper's `.onnx.json` config. Falls back to `StubTTS` (logs instead of speaking) if `tts.server_bin` unset in `models.yaml`.

### CLI
- `naira setup` — first-run parent consent gate (blocks `run` until accepted), text synced with `README.md`.
- `naira models list` / `naira models download`.
- `naira run` — stdin text-mode (default, for testing without hardware) and `--audio` (real capture → VAD → endpoint → STT → tag-router → TTS loop, subprocess-supervised STT/LLM spawned automatically if `server_bin` set in `models.yaml`). Face UI (see below) always starts alongside either mode.

### Face UI
- `internal/adapter/ui.Server` — loopback HTTP+WebSocket server (`--ui-port`, default 8090) implementing `domain.UIPublisher`; broadcasts `state_change`/`mouth_amplitude`/`window_mode`/`agent_status` (RFC.md#apis Internal IPC) to every connected client, no CGo.
- `internal/adapter/ui/static` — face client embedded into the binary via `go:embed`, full-bleed sprite frames (`assets/faces/`, 800x480, copied into `static/faces/`) driving 4 of the 9 `domain.ExpressionTag` states: IDLE (1 frame), LISTENING (2-frame blink loop), THINKING (4-frame cycle), SPEAKING (3-frame set, picked by live `mouth_amplitude` — not a fixed interval, so the mouth actually syncs to TTS output). A "Loading…" `warmup` sprite is the pre-connect splash. HAPPY/SURPRISED/SYMPATHETIC/WORKING/SLEEPY have no art yet and fall back to the original CSS-drawn face. `assets/faces/capturing` and `assets/faces/error` exist but aren't mapped to any state yet (no ExpressionTag fits "actively capturing" or "degraded/error" — see TODO #10 Subsystem-failure UX).
- Displayed by auto-launching a kiosk-mode browser (`--ui-browser-bin`, default `chromium --app=<url>`; set to `""` to open the URL manually instead).
- Server caches the last `state_change`/`window_mode` frame and replays it to newly-connected clients (`internal/adapter/ui.Server.lastState`/`lastMode`) — without this, a browser reload would get stuck showing the initial splash forever since broadcast-only WS has no history.
- **Known gap**: a plain browser tab can't do real OS-level frameless/transparent/always-on-top window management, so `window_mode`'s FLOATING mode only shrinks/corner-docks the face within the page rather than moving a true small overlay window (see RFC.md §5 Concerns).

### Audio cues
- `domain.SoundBoard` (new port) + `internal/adapter/sound.Board` — plays a random `.wav` from `assets/sounds/{greeting,ack,thinking}_sounds/` (embedded via `go:embed`, piped into `aplay`/`--sound-player-bin` via stdin, no temp files), gated behind `--sound` (default on). Best-effort: playback errors are logged, never abort the conversation turn.
- Wired into `internal/usecase/conversation.Service`: greeting plays once at `naira run` startup; ack plays (fire-and-forget) the moment an utterance is handed to the LLM; a thinking-loop plays on repeat until the LLM's first streamed sentence arrives, then stops (`Service.startThinkingLoop`). Ported from the reference `be-more-agent` Python implementation this project is inspired by — those `.wav` assets were already vendored (`assets/sounds/`) but unused until now.

### Docs
- `RFC.md` — full technical design, mermaid diagrams (architecture, sequence flows, rollout), kept in sync with every architectural decision made during implementation (CGo→subprocess, SQLite→flat JSON, VAD addition, etc).
- `README.md` — privacy/data-handling disclosure (parent-facing, matches `naira setup` output), architecture overview, build/run instructions.

### Verified via smoke test
- `setup` → `models list/download` → `run` (stdin) end-to-end, `state.json` output matches schema exactly (including correct empty-array vs. `null` handling — fixed a slice-cloning bug that collapsed empty slices to `null`).
- `run` with fake whisper-server/llama-server subprocesses: spawn, readiness poll, streamed response, correct `[TAG][TAG]` stripping before TTS, clean shutdown.
- Full capture→VAD→endpointing→STT pipeline against synthetic PCM (silence→tone→silence): correct pre-roll, correct single cutoff at 700ms (not double-triggered), correct WAV framing, correct STT round-trip.
- `--audio` mode fails cleanly (non-zero exit) when the capture binary is missing, rather than hanging.

## To Do

Roughly in the order they'll block progress:

1. **Claude CLI / OpenCode agent sandbox enforcement** — policy is specified (deps-only network, scoped filesystem writes) but not the enforcement primitive (Linux namespaces? restricted user + seccomp? container?). `StubAgent` always fails until this is decided.
2. **Auth/key storage for Claude CLI / OpenCode** — `StubAuth` always returns unauthorized. Needs OS keyring or `0600`-permission config file, per RFC Security Implications.
3. **`EXECUTE_AGENT` timeout** — no concrete value wired in yet (RFC suggests ~120s) plus child-facing fallback wording.
4. **Concurrent `EXECUTE_AGENT` request handling** — behavior undefined if a second request arrives mid-generation (queue vs. reject).
5. **Real-hardware benchmarking (Phase 1 blocker)** — `<2.0s` voice latency target (endpointing + STT + LLM first token) unverified on actual i5-2510M under `-t 2`; VAD timing constants (700ms silence / 300ms pre-roll / 20s cap) unvalidated against real child speech patterns; now also covers wake-word HTTP round-trip cost per 20ms frame (`internal/adapter/wakeword.HTTPDetector`) and per-sentence `piper` process-spawn latency (`internal/adapter/tts.PiperCLI`), both unbenchmarked on real hardware.
6. **Energy VAD robustness** — pure RMS-threshold classifier is more sensitive to background noise than a spectral classifier; revisit WebRTC VAD (small CGo lib) after real-room Phase 1 testing.
7. **`models.yaml` checksums** — `sha256` fields ship blank; must be filled in (or accept manual-copy fallback) before `naira models download` will auto-fetch anything. (N/A for the new `wakeword` entry — openWakeWord manages its own model cache, not fetched via this mechanism.)
8. **Screen-time `[SLEEPY]` behavior** — logging/threshold-check logic exists in the state service but isn't wired into the orchestrator loop or UI yet.
9. **Claude CLI cost/rate-limit exposure** — no budget or rate-limiting specified; a chatty child could trigger many generation requests.
10. **Subsystem-failure UX** — process supervisor marks a subsystem permanently unhealthy after exhausting restart attempts, but no UI-facing degraded-mode expression is wired to that yet.
11. **RFC.md header placeholders** — `Owner`/`Approver` still `_TBD_`.
12. **Custom "Hey Naira" wake-word model** — current wake-word engine (openWakeWord) is wired and functional but uses a stock pretrained phrase (`hey_jarvis_v0.1`); a custom "Hey Naira" model requires running openWakeWord's own training pipeline, not yet done.
13. **Real OS-level window management for the face UI** — `window_mode` (FLOATING vs FULLSCREEN) currently only changes CSS layout inside the browser tab; it doesn't move/resize/make-transparent an actual OS window (a true 250x250 always-on-top overlay per PRD §4.1). Needs native `webview` (CGo) or a Neutralinojs shell to do this for real.

## Explicitly Out of Scope (v1)

- Cloud-hosted STT/LLM/TTS fallback.
- Multi-user/multi-child profiles.
- Mobile companion app / remote parental dashboard.
- Concurrent `EXECUTE_AGENT` jobs (single job at a time).
- Custom wake-word training UI.
- Hardware assembly / physical enclosure design.
- Speaker/voice authentication (any voice can wake/command the device).
