package cmd

import (
	"fmt"

	"github.com/jamesonstone/rungrid/internal/agentinstructions"
	"github.com/jamesonstone/rungrid/internal/output"
	"github.com/spf13/cobra"
)

func newInstructionsCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use:     "instructions [project-path ...]",
		Aliases: []string{agentinstructions.AliasCommand},
		Short:   "Print coding-agent workspace wiring instructions (alias: agent-start)",
		Long: "Print a self-contained brief that a coding agent can use to wire the " +
			"selected projects into one portable Rungrid workspace.",
		Example: "  rungrid instructions . ../api ../web\n" +
			"  rungrid agent-start . ../api ../web",
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			document := agentinstructions.Build(opt.configPath, args)
			if opt.json {
				return output.WriteJSON(command.OutOrStdout(), "AgentInstructions", "", document, nil)
			}
			if opt.quiet {
				return nil
			}
			_, err := fmt.Fprint(command.OutOrStdout(), document.Instructions)
			return err
		},
	}
}
