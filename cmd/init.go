package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/onboarding"
	"github.com/jamesonstone/rungrid/internal/output"
	"github.com/spf13/cobra"
)

func newInitCommand(opt *options) *cobra.Command {
	var nonInteractive, force bool
	var inputPath, name, terminal, fromCompose string
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a portable workspace manifest",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			destination, err := filepath.Abs(opt.configPath)
			if err != nil {
				return err
			}
			root := filepath.Dir(destination)
			options := onboarding.Options{
				Root: root, Destination: destination, DraftPath: filepath.Join(root, ".rungrid.draft.json"),
				Force: force, FromCompose: fromCompose, Input: command.InOrStdin(), Output: command.OutOrStdout(), ErrorOutput: command.ErrOrStderr(),
			}
			var result onboarding.Result
			if nonInteractive {
				var content []byte
				if inputPath != "" {
					if inputPath == "-" {
						content, err = io.ReadAll(io.LimitReader(command.InOrStdin(), 8<<20))
					} else {
						content, err = os.ReadFile(inputPath)
					}
					if err != nil {
						return errs.Wrap(errs.ExitUsage, "RG1401", "read non-interactive manifest input", err)
					}
				}
				result, err = onboarding.NonInteractive(options, content, name, terminal)
			} else {
				if inputPath != "" || name != "" || terminal != "" {
					return errs.New(errs.ExitUsage, "RG1402", "--input, --name, and --terminal require --non-interactive")
				}
				result, err = onboarding.Interactive(options)
			}
			if err != nil {
				return err
			}
			if opt.json {
				return output.WriteJSON(command.OutOrStdout(), "Init", result.Manifest.Project.ID, map[string]any{"path": destination, "resumed": result.Resumed}, nil)
			}
			if !opt.quiet {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "wrote %s for project %s\n", destination, result.Manifest.Project.ID)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&nonInteractive, "non-interactive", false, "disable the Bubble Tea onboarding flow")
	command.Flags().StringVar(&inputPath, "input", "", "complete manifest input file, or - for stdin")
	command.Flags().StringVar(&name, "name", "", "project name for non-interactive discovery")
	command.Flags().StringVar(&terminal, "terminal", "", "terminal mode: warp or headless")
	command.Flags().StringVar(&fromCompose, "from-compose", "", "workspace-relative Compose file to discover")
	command.Flags().BoolVar(&force, "force", false, "replace an existing manifest after explicit selection")
	return command
}
