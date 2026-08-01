package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/jamesonstone/rungrid/internal/doctor"
	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/generate"
	"github.com/jamesonstone/rungrid/internal/lifecycle"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/output"
	"github.com/jamesonstone/rungrid/internal/planner"
	"github.com/jamesonstone/rungrid/internal/serviceexec"
	"github.com/jamesonstone/rungrid/internal/session"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
	"github.com/jamesonstone/rungrid/internal/terminalshell"
	"github.com/jamesonstone/rungrid/internal/versions"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	Dirty     = "unknown"
)

type options struct {
	configPath string
	localPath  string
	stateDir   string
	projectID  string
	json       bool
	noColor    bool
	quiet      bool
	verbose    bool
}

func Execute() error {
	root := newRootCommand()
	root.SetIn(os.Stdin)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	return root.Execute()
}

func ExitCode(err error) int { return errs.Code(err) }

func newRootCommand() *cobra.Command {
	opt := &options{}
	root := &cobra.Command{
		Use:           "rungrid",
		Short:         "Run a reproducible local development workspace",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	flags := root.PersistentFlags()
	flags.StringVar(&opt.configPath, "config", ".rungrid.yaml", "manifest path")
	flags.StringVar(&opt.localPath, "local", "", "local overlay path")
	flags.StringVar(&opt.stateDir, "state-dir", "", "state root override")
	flags.StringVar(&opt.projectID, "project", "", "select a known project id")
	flags.BoolVar(&opt.json, "json", false, "emit rungrid/output/v1 JSON")
	flags.BoolVar(&opt.noColor, "no-color", false, "disable ANSI color")
	flags.BoolVarP(&opt.quiet, "quiet", "q", false, "suppress non-error output")
	flags.BoolVarP(&opt.verbose, "verbose", "v", false, "include redacted diagnostics")

	root.AddCommand(
		newInitCommand(opt),
		newDoctorCommand(opt),
		newPlanCommand(opt),
		newGenerateCommand(opt),
		newUpCommand(opt),
		newOpenCommand(opt),
		newAttachCommand(opt),
		newVersionsCommand(opt),
		newStatusCommand(opt),
		newLogsCommand(opt),
		newSessionCommand(opt),
		newStartCommand(opt),
		newStopCommand(opt),
		newDownCommand(opt),
		newUninstallCommand(opt),
		newConfigCommand(opt),
		newCompletionCommand(root),
		newVersionCommand(opt),
		newInternalCommand(opt),
	)
	return root
}

func (o *options) load() (*manifest.Loaded, error) {
	return manifest.Load(o.configPath, o.localPath)
}

func (o *options) active(ctx context.Context) (lifecycle.Active, error) {
	projectID := o.projectID
	if projectID == "" {
		loaded, err := o.load()
		if err != nil {
			return lifecycle.Active{}, err
		}
		projectID = loaded.Manifest.Project.ID
	}
	return lifecycle.LoadActive(ctx, projectID, o.stateDir)
}

func newDoctorCommand(opt *options) *cobra.Command {
	var fix bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Validate configuration and local dependencies",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := opt.load()
			if err != nil {
				return err
			}
			report := doctor.Run(command.Context(), loaded, opt.stateDir, fix)
			if opt.json {
				if err := output.WriteJSON(command.OutOrStdout(), "DoctorReport", loaded.Manifest.Project.ID, report, nil); err != nil {
					return err
				}
			} else if !opt.quiet {
				for _, check := range report.Checks {
					_, _ = fmt.Fprintf(command.OutOrStdout(), "%-8s %-24s %s", check.Status, check.Name, check.Summary)
					if check.Detail != "" {
						_, _ = fmt.Fprintf(command.OutOrStdout(), " (%s)", check.Detail)
					}
					_, _ = fmt.Fprintln(command.OutOrStdout())
				}
			}
			if !report.OK {
				return errs.New(errs.ExitDependency, "RG1201", "doctor found blocking problems")
			}
			return nil
		},
	}
	command.Flags().BoolVar(&fix, "fix", false, "repair safe project-owned state")
	return command
}

func newPlanCommand(opt *options) *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Show deterministic generation and lifecycle actions",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := opt.load()
			if err != nil {
				return err
			}
			plan := planner.Build(loaded, Version)
			if opt.json || format == "json" {
				return output.WriteJSON(command.OutOrStdout(), "Plan", loaded.Manifest.Project.ID, plan, nil)
			}
			if format != "human" {
				return errs.New(errs.ExitUsage, "RG1202", "plan output must be human or json")
			}
			if !opt.quiet {
				plan.WriteHuman(command.OutOrStdout())
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "output", "human", "output format: human or json")
	return command
}

func newGenerateCommand(opt *options) *cobra.Command {
	var check bool
	command := &cobra.Command{
		Use:   "generate",
		Short: "Generate owned project runtime artifacts",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := opt.load()
			if err != nil {
				return err
			}
			result, err := generate.Run(loaded, opt.stateDir, Version, check)
			if err != nil {
				return err
			}
			if opt.json {
				return output.WriteJSON(command.OutOrStdout(), "Generation", loaded.Manifest.Project.ID, result, nil)
			}
			if !opt.quiet {
				verb := "reused"
				if result.Created {
					verb = "created"
				}
				_, _ = fmt.Fprintf(command.OutOrStdout(), "%s generation %s at %s\n", verb, result.Plan.GenerationID, result.Directory)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&check, "check", false, "verify generation without writing")
	return command
}

func newUpCommand(opt *options) *cobra.Command {
	var headless, noOpen bool
	command := &cobra.Command{
		Use:   "up [service ...]",
		Short: "Generate and start the detached workspace",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			loaded, err := opt.load()
			if err != nil {
				return err
			}
			open := loaded.Manifest.Terminal.Open != nil && *loaded.Manifest.Terminal.Open && !noOpen
			result, err := lifecycle.Up(command.Context(), loaded, lifecycle.UpOptions{
				StateOverride: opt.stateDir, GeneratorVersion: Version, Headless: headless, Open: open, Requested: args,
			})
			if err != nil {
				return err
			}
			if opt.json {
				return output.WriteJSON(command.OutOrStdout(), "Up", loaded.Manifest.Project.ID, result, nil)
			}
			if !opt.quiet {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "workspace is running (PID %d, generation %s)\n", result.RuntimePID, result.Generation)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&headless, "headless", false, "do not create or open terminal files")
	command.Flags().BoolVar(&noOpen, "no-open", false, "do not open Warp")
	return command
}

func newOpenCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use:   "open [service]",
		Short: "Open the Warp workspace or one service tab",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			service := ""
			if len(args) == 1 {
				service = args[0]
			}
			return lifecycle.Open(command.Context(), active, service)
		},
	}
}

func newAttachCommand(opt *options) *cobra.Command {
	var readOnly bool
	command := &cobra.Command{
		Use:   "attach",
		Short: "Attach to the active Process Compose TUI",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			return lifecycle.Attach(command.Context(), active, readOnly, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr())
		},
	}
	command.Flags().BoolVar(&readOnly, "read-only", true, "disable lifecycle mutations in the TUI")
	return command
}

func newVersionsCommand(opt *options) *cobra.Command {
	var watch, once bool
	command := &cobra.Command{
		Use:   "versions",
		Short: "Show process, listener, and source-control state",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if watch && once {
				return errs.New(errs.ExitUsage, "RG1203", "--watch and --once are mutually exclusive")
			}
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			client := supervisor.Client(active.Layout, active.Runtime)
			if !watch && !once && !opt.json {
				watch = isTerminalOutput(command.OutOrStdout())
			}
			if opt.json || once || !watch {
				snapshot := versions.Capture(command.Context(), active.Manifest, active.Runtime, client)
				if opt.json {
					return output.WriteJSON(command.OutOrStdout(), "Versions", active.Layout.ProjectID, snapshot, nil)
				}
				versions.WriteHuman(command.OutOrStdout(), snapshot)
				return nil
			}
			return watchVersions(command, active)
		},
	}
	command.Flags().BoolVar(&watch, "watch", false, "refresh continuously")
	command.Flags().BoolVar(&once, "once", false, "print one snapshot")
	return command
}

func isTerminalOutput(writer any) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func watchVersions(command *cobra.Command, active lifecycle.Active) error {
	ctx, cancel := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	first := true
	for {
		if !first {
			_, _ = fmt.Fprint(command.OutOrStdout(), "\033[H\033[J")
		}
		first = false
		versions.WriteHuman(command.OutOrStdout(), versions.Capture(ctx, active.Manifest, active.Runtime, supervisor.Client(active.Layout, active.Runtime)))
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func newStatusCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status [service ...]",
		Short: "Report active runtime and service state",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			states, _, err := lifecycle.Status(command.Context(), active)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				wanted := map[string]bool{}
				for _, name := range args {
					wanted[name] = true
				}
				filtered := states[:0]
				for _, item := range states {
					if wanted[item.Name] {
						filtered = append(filtered, item)
					}
				}
				states = filtered
			}
			data := map[string]any{"generation": active.Runtime.GenerationID, "pid": active.Runtime.PID, "socket": active.Runtime.Socket, "services": states}
			if opt.json {
				return output.WriteJSON(command.OutOrStdout(), "Status", active.Layout.ProjectID, data, nil)
			}
			if !opt.quiet {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "runtime PID %d  generation %s\n", active.Runtime.PID, active.Runtime.GenerationID)
				for _, item := range states {
					_, _ = fmt.Fprintf(command.OutOrStdout(), "%-20s %-10s %-9s %-14s pid=%d health=%s session=%t tab=%t\n", item.Name, item.Source, item.Activation, item.Status, item.PID, item.Health, item.SessionOwned, item.TabRegistered)
				}
			}
			return nil
		},
	}
}

func newLogsCommand(opt *options) *cobra.Command {
	var follow, raw bool
	var tail int
	command := &cobra.Command{
		Use:   "logs [service ...]",
		Short: "Read Process Compose service logs",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			return lifecycle.Logs(command.Context(), active, args, follow, tail, raw, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr())
		},
	}
	command.Flags().BoolVarP(&follow, "follow", "f", false, "follow new log output")
	command.Flags().IntVar(&tail, "tail", -1, "number of lines from the end")
	command.Flags().BoolVar(&raw, "raw", false, "omit service prefixes")
	return command
}

func newSessionCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use:   "session <service>",
		Short: "Own a tab service until stop or signal",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
			defer cancel()
			return session.Run(ctx, session.Options{Layout: active.Layout, Runtime: active.Runtime, Manifest: active.Manifest, Service: args[0], TabID: os.Getenv("WARP_PANE_ID"), Stdin: command.InOrStdin(), Stdout: command.OutOrStdout(), Stderr: command.ErrOrStderr()})
		},
	}
}

func newStartCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use: "start <service>", Short: "Start a service using activation-aware behavior", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			message, err := lifecycle.Start(command.Context(), active, args[0])
			if err != nil {
				return err
			}
			if !opt.quiet {
				_, _ = fmt.Fprintln(command.OutOrStdout(), message)
			}
			return nil
		},
	}
}

func newStopCommand(opt *options) *cobra.Command {
	return &cobra.Command{
		Use: "stop <service>", Short: "Stop an exact managed service", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			return lifecycle.Stop(command.Context(), active, args[0])
		},
	}
}

func newDownCommand(opt *options) *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use: "down", Short: "Perform ordered workspace shutdown", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				if errs.Code(err) == errs.ExitConflict && strings.Contains(err.Error(), "no active") {
					return nil
				}
				return err
			}
			ctx := command.Context()
			cancel := func() {}
			if timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, timeout)
			}
			defer cancel()
			return lifecycle.Down(ctx, active)
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 0, "overall shutdown timeout")
	return command
}

func newUninstallCommand(opt *options) *cobra.Command {
	var keepLogs, keepConfig bool
	command := &cobra.Command{
		Use: "uninstall", Short: "Remove only owned project state and Warp files", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			projectID := opt.projectID
			if projectID == "" {
				loaded, err := opt.load()
				if err != nil {
					return err
				}
				projectID = loaded.Manifest.Project.ID
			}
			layout, err := state.NewLayout(projectID, opt.stateDir)
			if err != nil {
				return err
			}
			return lifecycle.Uninstall(command.Context(), layout, keepLogs, keepConfig)
		},
	}
	command.Flags().BoolVar(&keepLogs, "keep-logs", false, "preserve project logs")
	command.Flags().BoolVar(&keepConfig, "keep-config", false, "preserve generated configuration")
	return command
}

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

func newInternalCommand(opt *options) *cobra.Command {
	internal := &cobra.Command{Use: "internal", Hidden: true}
	internal.AddCommand(newExecServiceCommand(opt, false), newExecServiceCommand(opt, true), newServiceShellCommand(opt), newTriggerCommand(opt))
	return internal
}

func newExecServiceCommand(opt *options, health bool) *cobra.Command {
	var projectID, generation, service string
	name := "exec-service"
	if health {
		name = "health-service"
	}
	command := &cobra.Command{
		Use: name, Args: cobra.NoArgs, Hidden: true,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, err := serviceexec.LoadContext(projectID, generation, opt.stateDir, "")
			if err != nil {
				return err
			}
			if health {
				return serviceexec.CheckHealth(command.Context(), ctx, service)
			}
			return serviceexec.Exec(command.Context(), ctx, service)
		},
	}
	command.Flags().StringVar(&projectID, "project-id", "", "project id")
	command.Flags().StringVar(&generation, "generation", "", "generation id")
	command.Flags().StringVar(&service, "service", "", "service name")
	_ = command.MarkFlagRequired("project-id")
	_ = command.MarkFlagRequired("generation")
	_ = command.MarkFlagRequired("service")
	return command
}

func newServiceShellCommand(opt *options) *cobra.Command {
	var generation, service string
	command := &cobra.Command{
		Use: "service-shell", Hidden: true, Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			if active.Runtime.GenerationID != generation {
				return errs.New(errs.ExitConflict, "RG1206", "Warp tab generation is stale")
			}
			return terminalshell.RunShell(command.Context(), terminalshell.ShellOptions{Layout: active.Layout, Runtime: active.Runtime, Manifest: active.Manifest, Service: service, Stdin: command.InOrStdin(), Stdout: command.OutOrStdout(), Stderr: command.ErrOrStderr()})
		},
	}
	command.Flags().StringVar(&generation, "generation", "", "generation id")
	command.Flags().StringVar(&service, "service", "", "service name")
	_ = command.MarkFlagRequired("generation")
	_ = command.MarkFlagRequired("service")
	return command
}

func newTriggerCommand(opt *options) *cobra.Command {
	var generation, service string
	command := &cobra.Command{
		Use: "trigger", Hidden: true, Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			active, err := opt.active(command.Context())
			if err != nil {
				return err
			}
			if active.Runtime.GenerationID != generation {
				return errs.New(errs.ExitConflict, "RG1207", "managed trigger generation is stale")
			}
			ctx, cancel := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
			defer cancel()
			return terminalshell.RunTrigger(ctx, active.Layout, active.Runtime, active.Manifest, service, args, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr())
		},
	}
	command.Flags().StringVar(&generation, "generation", "", "generation id")
	command.Flags().StringVar(&service, "service", "", "service name")
	_ = command.MarkFlagRequired("generation")
	_ = command.MarkFlagRequired("service")
	return command
}

func redactManifest(value manifest.Manifest) manifest.Manifest {
	copyValue := value
	copyValue.Services = append([]manifest.Service(nil), value.Services...)
	for i := range copyValue.Services {
		if len(copyValue.Services[i].Environment.Values) > 0 {
			redacted := map[string]string{}
			for key := range copyValue.Services[i].Environment.Values {
				redacted[key] = "<redacted>"
			}
			copyValue.Services[i].Environment.Values = redacted
		}
	}
	return copyValue
}
