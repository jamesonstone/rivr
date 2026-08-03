package cmd

import (
	"fmt"
	"runtime"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/output"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newConfigCommand(opt *options) *cobra.Command {
	config := &cobra.Command{Use: "config", Short: "Inspect and validate manifest configuration"}
	var merged, redacted bool
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if !merged {
			return errs.New(errs.ExitUsage, "RG1208", "config show is defined over the validated merged manifest")
		}
		loaded, err := opt.load()
		if err != nil {
			return err
		}
		value := loaded.Manifest
		if redacted {
			value = redactManifest(value)
		}
		content, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = command.OutOrStdout().Write(content)
		return err
	}}
	show.Flags().BoolVar(&merged, "merged", true, "show imports and local overrides merged")
	show.Flags().BoolVar(&redacted, "redacted", true, "redact literal environment values")
	config.AddCommand(
		&cobra.Command{Use: "validate", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := opt.load()
			if err != nil {
				return err
			}
			if !opt.quiet {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "valid %s workspace %s\n", manifest.APIVersion, loaded.Manifest.Project.ID)
			}
			return nil
		}},
		show,
		&cobra.Command{Use: "schema", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
			_, err := command.OutOrStdout().Write(manifest.Schema())
			return err
		}},
		&cobra.Command{Use: "path", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := opt.load()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), loaded.ConfigPath)
			return err
		}},
	)
	return config
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use: "completion bash|zsh|fish", Short: "Generate shell completion", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(command.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(command.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(command.OutOrStdout(), true)
			default:
				return errs.New(errs.ExitUsage, "RG1205", "completion shell must be bash, zsh, or fish")
			}
		},
	}
}

func newVersionCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use: "version", Short: "Show build and API compatibility", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			data := map[string]any{"version": Version, "commit": Commit, "build_time": BuildTime, "dirty": Dirty, "go": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH, "manifest_api": manifest.APIVersion, "output_api": output.APIVersion}
			if opt.json {
				return output.WriteJSON(command.OutOrStdout(), "Version", "", data, nil)
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "rungrid %s (%s) %s %s\n", Version, Commit, runtime.GOOS+"/"+runtime.GOARCH, BuildTime)
			return err
		},
	}
}

func redactManifest(value manifest.Manifest) manifest.Manifest {
	copyValue := value
	copyValue.Services = append([]manifest.Service(nil), value.Services...)
	for i := range copyValue.Services {
		copyValue.Services[i].Environment = redactedEnvironment(copyValue.Services[i].Environment)
	}
	copyValue.Lifecycle.BeforeUp = append([]manifest.LifecycleCommand(nil), value.Lifecycle.BeforeUp...)
	for i := range copyValue.Lifecycle.BeforeUp {
		copyValue.Lifecycle.BeforeUp[i].Environment = redactedEnvironment(copyValue.Lifecycle.BeforeUp[i].Environment)
	}
	copyValue.Lifecycle.AfterDown = append([]manifest.LifecycleCommand(nil), value.Lifecycle.AfterDown...)
	for i := range copyValue.Lifecycle.AfterDown {
		copyValue.Lifecycle.AfterDown[i].Environment = redactedEnvironment(copyValue.Lifecycle.AfterDown[i].Environment)
	}
	return copyValue
}

func redactedEnvironment(value manifest.Environment) manifest.Environment {
	if len(value.Values) == 0 {
		return value
	}
	copyValue := value
	copyValue.Values = make(map[string]string, len(value.Values))
	for key := range value.Values {
		copyValue.Values[key] = "<redacted>"
	}
	return copyValue
}
