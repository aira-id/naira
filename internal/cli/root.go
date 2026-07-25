// Package cli wires the cobra command tree and viper-backed configuration
// (per project convention: cobra for command management, viper for config
// parsing) on top of the usecase/adapter layers.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"naira/internal/config"
)

// Execute builds and runs the root command.
func Execute() error {
	root := newRootCmd()
	return root.Execute()
}

func newRootCmd() *cobra.Command {
	var homeFlag string

	root := &cobra.Command{
		Use:           "naira",
		Short:         "Naira — offline-first AI companion robot orchestrator",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringVar(&homeFlag, "home", "", "runtime directory holding state.json, models.yaml, models/, logs/ (default ~/.naira, env NAIRA_HOME)")
	_ = viper.BindPFlag("home", root.PersistentFlags().Lookup("home"))
	_ = viper.BindEnv("home", "NAIRA_HOME")

	root.AddCommand(newModelsCmd())
	root.AddCommand(newSetupCmd())
	root.AddCommand(newRunCmd())

	return root
}

// resolveHome returns the runtime directory: --home flag, else NAIRA_HOME
// env, else ~/.naira — creating it if necessary.
func resolveHome() (string, error) {
	if v := viper.GetString("home"); v != "" {
		return v, nil
	}
	home, err := config.EnsureHome()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}
