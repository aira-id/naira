package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"naira/internal/adapter/repository"
	"naira/internal/config"
	"naira/internal/idgen"
	statesvc "naira/internal/usecase/state"
)

// consentDisclosure is the parent-facing privacy/data-handling disclosure.
// It must be shown and explicitly accepted before setup completes
// (RFC.md#rollout-strategy, #security-implications). Keep this in sync with
// the "Privacy & Data Handling" section of README.md.
const consentDisclosure = `
Naira Privacy & Data Handling Disclosure (v` + statesvc.DisclosureVersion + `)

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
  (~/.naira/state.json) and is never transmitted anywhere.

By continuing, you acknowledge this disclosure on behalf of any child using
this device.
`

func newSetupCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "First-run parent consent flow (required before the device leaves setup mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolveHome()
			if err != nil {
				return err
			}

			repo := repository.NewStateJSON(config.StatePath(home))
			svc := statesvc.New(repo)
			if err := svc.Load(cmd.Context()); err != nil {
				return err
			}

			if svc.IsConsented() {
				fmt.Fprintln(cmd.OutOrStdout(), "Parent consent already recorded — nothing to do.")
				return nil
			}

			fmt.Fprint(cmd.OutOrStdout(), consentDisclosure)

			if !yes {
				fmt.Fprint(cmd.OutOrStdout(), "Do you acknowledge this disclosure and wish to continue? [y/N]: ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				answer := strings.ToLower(strings.TrimSpace(line))
				if answer != "y" && answer != "yes" {
					return fmt.Errorf("setup cannot continue without parent acknowledgment")
				}
			}

			if err := svc.AcceptConsent(cmd.Context(), idgen.New()); err != nil {
				return fmt.Errorf("record consent: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Thank you — consent recorded. Run `naira models download` next, then `naira run`.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "accept the disclosure non-interactively (for scripted installs)")
	return cmd
}
