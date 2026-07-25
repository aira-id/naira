package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"naira/internal/adapter/audio"
	"naira/internal/adapter/engine"
	"naira/internal/adapter/network"
	"naira/internal/adapter/process"
	"naira/internal/adapter/repository"
	"naira/internal/adapter/tts"
	"naira/internal/adapter/vad"
	"naira/internal/adapter/wakeword"
	"naira/internal/config"
	"naira/internal/domain"
	convsvc "naira/internal/usecase/conversation"
	"naira/internal/usecase/listening"
	statesvc "naira/internal/usecase/state"
)

func newRunCmd() *cobra.Command {
	var audioMode bool
	var micBin string
	var micArgs []string
	var ttsPlayerBin string
	var ttsPlayerArgs []string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the core orchestrator loop",
		Long: `Start the core orchestrator loop.

STT/LLM/wake-word run as standalone subprocesses (whisper-server,
llama-server, scripts/openwakeword_server.py), supervised (spawned,
health-polled, auto-restarted) per models.yaml's server_bin/port/args — see
RFC.md#architecture--tech-stack decision note. TTS (piper) is also a
standalone subprocess but spawned fresh per sentence instead of supervised,
since Piper ships no server mode; output is piped into --tts-player-bin
(default aplay). If server_bin is unset for a subsystem, it falls back to a
stub (wake-word: NoOp, never fires; TTS: logs instead of speaking) so the
rest of the orchestrator remains exercisable without those binaries
installed.

Default mode reads plain-text lines from stdin in place of microphone input.
--audio switches to real capture (arecord subprocess) through the
VAD-endpointed listening pipeline. Wake-word detection uses openWakeWord
(stock pretrained phrase, e.g. "hey jarvis" — no custom "hey naira" model
trained yet, see RFC.md §5 Concerns) when wakeword.server_bin is set;
otherwise nothing will be transcribed, by design — the mic-always-open/
wake-word-gated privacy guarantee must not be bypassed by a CLI flag.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolveHome()
			if err != nil {
				return err
			}

			stateRepo := repository.NewStateJSON(config.StatePath(home))
			stateSvc := statesvc.New(stateRepo)
			if err := stateSvc.Load(cmd.Context()); err != nil {
				return fmt.Errorf("load state: %w", err)
			}

			if !stateSvc.IsConsented() {
				return fmt.Errorf("parent consent not recorded — run `naira setup` first")
			}

			modelsYAMLPath := config.ModelsYAMLPath(home)
			if err := repository.EnsureDefault(modelsYAMLPath); err != nil {
				return fmt.Errorf("ensure default models.yaml: %w", err)
			}
			modelsCfg, err := repository.NewModelsYAML(modelsYAMLPath).Load(cmd.Context())
			if err != nil {
				return fmt.Errorf("load models.yaml: %w", err)
			}

			var sttEngine domain.STTEngine = engine.StubSTT{}
			var llmEngine domain.LLMEngine = engine.StubLLM{}
			var supervisors []*process.Supervisor
			defer func() {
				for _, sup := range supervisors {
					sup.Stop()
				}
			}()

			if modelsCfg.STT.HasServer() {
				if _, statErr := os.Stat(modelsCfg.STT.Path); statErr != nil {
					return fmt.Errorf("stt model file missing at %s: refusing to start whisper-server", modelsCfg.STT.Path)
				}
				sup := process.New("whisper-server", modelsCfg.STT.ServerBin, serverArgs(modelsCfg.STT), modelsCfg.STT.Port)
				if err := sup.Start(cmd.Context()); err != nil {
					return fmt.Errorf("start whisper-server: %w", err)
				}
				supervisors = append(supervisors, sup)
				sttEngine = engine.NewWhisperServerSTT(fmt.Sprintf("http://127.0.0.1:%d", modelsCfg.STT.Port))
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: stt.server_bin not set in models.yaml — STT disabled (stub)")
			}

			var wakeDetector domain.WakeWordDetector = wakeword.NoOp{}
			if modelsCfg.WakeWord.HasServer() {
				sup := process.New("openwakeword-server", modelsCfg.WakeWord.ServerBin, wakewordServerArgs(modelsCfg.WakeWord), modelsCfg.WakeWord.Port)
				if err := sup.Start(cmd.Context()); err != nil {
					return fmt.Errorf("start openwakeword-server: %w", err)
				}
				supervisors = append(supervisors, sup)
				wakeDetector = wakeword.NewHTTPDetector(fmt.Sprintf("http://127.0.0.1:%d", modelsCfg.WakeWord.Port))
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: wakeword.server_bin not set in models.yaml — wake-word disabled (stub, never fires)")
			}

			if modelsCfg.LLM.HasServer() {
				if _, statErr := os.Stat(modelsCfg.LLM.Path); statErr != nil {
					return fmt.Errorf("llm model file missing at %s: refusing to start llama-server", modelsCfg.LLM.Path)
				}
				sup := process.New("llama-server", modelsCfg.LLM.ServerBin, serverArgs(modelsCfg.LLM), modelsCfg.LLM.Port)
				if err := sup.Start(cmd.Context()); err != nil {
					return fmt.Errorf("start llama-server: %w", err)
				}
				supervisors = append(supervisors, sup)
				llmEngine = engine.NewLlamaServerLLM(fmt.Sprintf("http://127.0.0.1:%d", modelsCfg.LLM.Port))
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: llm.server_bin not set in models.yaml — LLM disabled (stub)")
			}

			var ttsEngine domain.TTSEngine = engine.StubTTS{}
			if modelsCfg.TTS.HasServer() {
				if _, statErr := os.Stat(modelsCfg.TTS.Path); statErr != nil {
					return fmt.Errorf("tts model file missing at %s: refusing to start piper", modelsCfg.TTS.Path)
				}
				ttsEngine = tts.NewPiperCLI(modelsCfg.TTS.ServerBin, modelsCfg.TTS.Path, modelsCfg.TTS.ConfigPath, modelsCfg.TTS.Args, ttsPlayerBin, ttsPlayerArgs)
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: tts.server_bin not set in models.yaml — TTS disabled (stub, logs instead of speaking)")
			}

			engines := convsvc.Engines{
				STT:          sttEngine,
				LLM:          llmEngine,
				TTS:          ttsEngine,
				Agent:        engine.StubAgent{},
				UI:           engine.StubUI{},
				Connectivity: network.NewChecker(),
				Auth:         engine.StubAuth{},
			}
			orchestrator := convsvc.New(engines, stateSvc, config.GamesDir(home))

			sessionID, err := stateSvc.StartSession(cmd.Context())
			if err != nil {
				return fmt.Errorf("start session: %w", err)
			}

			if audioMode {
				runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()

				capture := audio.NewMicCapture(micBin, micArgs)
				listener := listening.New(capture, wakeDetector, vad.NewEnergy(), sttEngine, listening.DefaultOptions())

				fmt.Fprintln(cmd.OutOrStdout(), "Naira orchestrator running in --audio mode. Ctrl+C to stop.")
				wakeCount := 0
				runErr := listener.Run(runCtx, func(ctx context.Context, transcript string) {
					wakeCount++
					if err := orchestrator.HandleUtterance(ctx, sessionID, transcript); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
					}
				})
				endErr := stateSvc.EndSession(cmd.Context(), sessionID, wakeCount)
				if runErr != nil && !errors.Is(runErr, context.Canceled) {
					return fmt.Errorf("listening error: %w", runErr)
				}
				return endErr
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Naira orchestrator running. Type an utterance and press enter; Ctrl+D to stop.")
			scanner := bufio.NewScanner(cmd.InOrStdin())
			wakeCount := 0
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				wakeCount++
				if err := orchestrator.HandleUtterance(cmd.Context(), sessionID, line); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				}
			}

			return stateSvc.EndSession(cmd.Context(), sessionID, wakeCount)
		},
	}

	cmd.Flags().BoolVar(&audioMode, "audio", false, "capture real microphone audio via a subprocess (arecord) instead of reading stdin lines")
	cmd.Flags().StringVar(&micBin, "mic-bin", "arecord", "recording subprocess binary (arecord/parec)")
	cmd.Flags().StringSliceVar(&micArgs, "mic-args", nil, "extra args passed to the recording subprocess (e.g. -D plughw:1,0)")
	cmd.Flags().StringVar(&ttsPlayerBin, "tts-player-bin", "aplay", "playback subprocess binary for synthesized speech")
	cmd.Flags().StringSliceVar(&ttsPlayerArgs, "tts-player-args", nil, "extra args passed to the playback subprocess (e.g. -D plughw:1,0)")
	return cmd
}

// serverArgs builds the standard whisper-server/llama-server invocation:
// -m <model path> --port <port> --host 127.0.0.1 (hardcoded loopback-only
// per RFC.md Security Implications), followed by any extra flags from
// models.yaml (e.g. -t 2 -c 1024 --mlock for llama-server).
func serverArgs(entry domain.ModelEntry) []string {
	args := []string{"-m", entry.Path, "--port", strconv.Itoa(entry.Port), "--host", "127.0.0.1"}
	return append(args, entry.Args...)
}

// wakewordServerArgs builds the openwakeword_server.py invocation: entry.Args
// carries the script path plus --model/--cache-dir/--threshold (its shape
// differs from whisper-server/llama-server's flags, see models.yaml), with
// --port appended so it always matches the port the supervisor polls.
func wakewordServerArgs(entry domain.ModelEntry) []string {
	args := append([]string{}, entry.Args...)
	return append(args, "--port", strconv.Itoa(entry.Port))
}
