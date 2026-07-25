package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"naira/internal/adapter/downloader"
	"naira/internal/adapter/network"
	"naira/internal/adapter/repository"
	"naira/internal/config"
	"naira/internal/domain"
	modelsvc "naira/internal/usecase/models"
)

func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage STT/LLM/TTS model files (models.yaml)",
	}
	cmd.AddCommand(newModelsListCmd())
	cmd.AddCommand(newModelsDownloadCmd())
	return cmd
}

func newModelsService() (*modelsvc.Service, error) {
	home, err := resolveHome()
	if err != nil {
		return nil, err
	}
	yamlPath := config.ModelsYAMLPath(home)
	if err := repository.EnsureDefault(yamlPath); err != nil {
		return nil, fmt.Errorf("ensure default models.yaml: %w", err)
	}

	repo := repository.NewModelsYAML(yamlPath)
	conn := network.NewChecker()
	dl := downloader.NewHTTP()
	return modelsvc.New(repo, conn, dl), nil
}

func newModelsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show configured vs. present-on-disk status per subsystem",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newModelsService()
			if err != nil {
				return err
			}
			statuses, err := svc.List(cmd.Context())
			if err != nil {
				return err
			}
			for _, s := range statuses {
				presence := "MISSING"
				if s.Present {
					presence = "present"
				}
				fetch := ""
				if !s.Fetchable {
					fetch = " (not auto-fetchable — no url/sha256)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-28s %-8s %s%s\n", s.Subsystem, s.Name, presence, s.Path, fetch)
			}
			return nil
		},
	}
}

func newModelsDownloadCmd() *cobra.Command {
	var only string
	var force bool

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Fetch missing/mismatched models per models.yaml, verifying sha256",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newModelsService()
			if err != nil {
				return err
			}

			opts := modelsvc.DownloadOptions{Force: force}
			if only != "" {
				sub := domain.Subsystem(only)
				if sub != domain.SubsystemSTT && sub != domain.SubsystemLLM && sub != domain.SubsystemTTS {
					return fmt.Errorf("--only must be one of stt|llm|tts, got %q", only)
				}
				opts.Only = sub
			}

			results, err := svc.Download(cmd.Context(), opts)
			if err != nil {
				return err
			}

			failed := false
			for _, r := range results {
				switch {
				case r.Err != nil:
					failed = true
					fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-28s ERROR: %v\n", r.Subsystem, r.Name, r.Err)
				case r.Skipped:
					fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-28s skipped (%s)\n", r.Subsystem, r.Name, r.Message)
				default:
					fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-28s %s\n", r.Subsystem, r.Name, r.Message)
				}
			}
			if failed {
				return fmt.Errorf("one or more models failed to download")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&only, "only", "", "restrict to one subsystem: stt|llm|tts")
	cmd.Flags().BoolVar(&force, "force", false, "re-download even if already present")
	return cmd
}
