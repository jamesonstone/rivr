package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/subprocess"
	"github.com/jamesonstone/rungrid/internal/workspace"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

func Run(ctx context.Context, loaded *manifest.Loaded, stateOverride string, fix bool) Report {
	report := Report{OK: true}
	add := func(check Check) {
		report.Checks = append(report.Checks, check)
		if check.Status == "error" {
			report.OK = false
		}
	}
	add(Check{Name: "manifest", Status: "ok", Summary: "manifest is valid"})
	add(Check{
		Name: "workspace-root", Status: "ok",
		Summary: "relative workspace root is valid and contains the manifest directory",
		Detail:  loaded.Manifest.Workspace.Root,
	})
	for _, name := range manifest.DeclaredRepositoryNames(&loaded.Manifest) {
		repositoryRoot, repositoryErr := manifest.RepositoryRoot(&loaded.Manifest, loaded.WorkspaceRoot, name)
		if repositoryErr != nil {
			add(Check{Name: "repository:" + name, Status: "error", Summary: "declared repository root is unavailable", Detail: repositoryErr.Error()})
			continue
		}
		add(Check{Name: "repository:" + name, Status: "ok", Summary: "declared repository root is valid", Detail: loaded.Manifest.Repositories[name].Path})
		gitCommand := exec.CommandContext(ctx, "git", "-C", repositoryRoot, "rev-parse", "--show-toplevel")
		if err := gitCommand.Run(); err != nil {
			add(Check{Name: "repository-git:" + name, Status: "warning", Summary: "source-control state is unavailable"})
		} else {
			add(Check{Name: "repository-git:" + name, Status: "ok", Summary: "source-control state is available"})
		}
	}

	pc := loaded.Manifest.Runtime.ProcessCompose.Executable
	if resolved, err := exec.LookPath(pc); err != nil {
		add(Check{Name: "process-compose", Status: "error", Summary: "Process Compose executable was not found", Detail: pc})
	} else {
		version, versionErr := processComposeVersion(ctx, resolved)
		if versionErr != nil {
			add(Check{Name: "process-compose", Status: "error", Summary: "Process Compose version could not be determined", Detail: versionErr.Error()})
		} else if !processcompose.SupportedVersion(version) {
			add(Check{Name: "process-compose", Status: "error", Summary: "Process Compose version is outside >=1.120.0,<2.0.0", Detail: version})
		} else {
			add(Check{Name: "process-compose", Status: "ok", Summary: "Process Compose is compatible", Detail: version})
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		add(Check{Name: "github-cli", Status: "warning", Summary: "GitHub CLI is unavailable; worktree prune cannot prove merged pull requests"})
	} else if err := githubCLIAuthentication(ctx); err != nil {
		add(Check{Name: "github-cli", Status: "warning", Summary: "GitHub CLI authentication for github.com is unavailable; worktree prune cannot prove merged pull requests"})
	} else {
		add(Check{Name: "github-cli", Status: "ok", Summary: "GitHub CLI is authenticated for worktree prune proof"})
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		add(Check{Name: "lsof", Status: "warning", Summary: "lsof is unavailable; worktree prune cannot prove that candidates are unused"})
	} else {
		add(Check{Name: "lsof", Status: "ok", Summary: "lsof is available for worktree process proof"})
	}

	required := requiredExecutables(&loaded.Manifest)
	for _, executable := range required {
		if _, err := exec.LookPath(executable); err != nil {
			add(Check{Name: "executable:" + executable, Status: "error", Summary: "required executable was not found"})
		} else {
			add(Check{Name: "executable:" + executable, Status: "ok", Summary: "required executable is available"})
		}
	}
	phases := []struct {
		name     string
		commands []manifest.LifecycleCommand
	}{
		{name: "before_up", commands: loaded.Manifest.Lifecycle.BeforeUp},
		{name: "after_down", commands: loaded.Manifest.Lifecycle.AfterDown},
	}
	for _, phase := range phases {
		for _, lifecycleCommand := range phase.commands {
			name := "lifecycle:" + phase.name + ":" + lifecycleCommand.Name
			if err := lifecycleExecutable(loaded.WorkspaceRoot, lifecycleCommand); err != nil {
				add(Check{Name: name, Status: "error", Summary: "lifecycle executable is unavailable", Detail: err.Error()})
			} else {
				add(Check{Name: name, Status: "ok", Summary: "lifecycle executable is available"})
			}
		}
	}

	if loaded.Manifest.Terminal.Mode == "warp" {
		if runtime.GOOS != "darwin" {
			add(Check{Name: "warp", Status: "error", Summary: "Warp graphical mode requires macOS in v1"})
		} else if _, err := os.Stat("/Applications/Warp.app"); err != nil {
			add(Check{Name: "warp", Status: "error", Summary: "Warp application was not found"})
		} else {
			add(Check{Name: "warp", Status: "ok", Summary: "Warp application is available"})
		}
		if _, err := exec.LookPath("zsh"); err != nil {
			add(Check{Name: "zsh", Status: "error", Summary: "zsh is required for Warp service tabs"})
		} else {
			add(Check{Name: "zsh", Status: "ok", Summary: "zsh is available"})
		}
	}

	layout, err := state.NewLayout(loaded.Manifest.Project.ID, stateOverride)
	if err != nil {
		add(Check{Name: "state", Status: "error", Summary: "state location is invalid", Detail: err.Error()})
	} else if fix {
		if err := layout.Ensure(); err != nil {
			add(Check{Name: "state", Status: "error", Summary: "state directory could not be repaired", Detail: err.Error()})
		} else {
			add(Check{Name: "state", Status: "ok", Summary: "project state directory is private and ready"})
		}
	} else if info, statErr := os.Lstat(layout.ProjectDir); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			add(Check{Name: "state", Status: "error", Summary: "project state directory ownership or permissions are unsafe"})
		} else {
			add(Check{Name: "state", Status: "ok", Summary: "project state directory is private"})
		}
	} else if os.IsNotExist(statErr) {
		add(Check{Name: "state", Status: "warning", Summary: "project state directory has not been generated"})
	} else {
		add(Check{Name: "state", Status: "error", Summary: "project state directory cannot be inspected", Detail: statErr.Error()})
	}
	if err == nil {
		if journal, exists, journalErr := workspace.ReadJournalIfPresent(layout); journalErr != nil {
			add(Check{Name: "lifecycle", Status: "error", Summary: "lifecycle journal is invalid", Detail: journalErr.Error()})
		} else if exists && journal.TeardownRequired {
			add(Check{Name: "lifecycle", Status: "warning", Summary: "workspace teardown remains required", Detail: journal.State})
		} else if exists {
			add(Check{Name: "lifecycle", Status: "ok", Summary: "lifecycle journal is consistent", Detail: journal.State})
		}
	}
	return report
}

func requiredExecutables(m *manifest.Manifest) []string {
	seen := map[string]bool{}
	var result []string
	add := func(value string) {
		if value == "" || strings.ContainsRune(value, os.PathSeparator) || seen[value] {
			return
		}
		seen[value] = true
		result = append(result, value)
	}
	for _, service := range m.Services {
		if service.Run != nil {
			for _, executable := range manifest.CommandExecutables(service.Run.Argv) {
				add(executable)
			}
		}
		if service.Compose != nil {
			for _, executable := range manifest.CommandExecutables(service.Compose.UpArgv) {
				add(executable)
			}
		}
		if service.External != nil && service.External.Command != nil {
			for _, executable := range manifest.CommandExecutables(service.External.Command.Argv) {
				add(executable)
			}
		}
		if service.Health != nil && service.Health.Command != nil {
			for _, executable := range manifest.CommandExecutables(service.Health.Command.Argv) {
				add(executable)
			}
		}
		if service.Activation == "tab" && len(service.Terminal.TriggerArgv) > 0 {
			add(service.Terminal.TriggerArgv[0])
		}
		for _, provider := range service.Environment.Providers {
			if provider.Type == "command" {
				for _, executable := range manifest.CommandExecutables(provider.Argv) {
					add(executable)
				}
			}
			if provider.Type == "direnv" {
				add("direnv")
			}
		}
	}
	commands := append(append([]manifest.LifecycleCommand(nil), m.Lifecycle.BeforeUp...), m.Lifecycle.AfterDown...)
	for _, command := range commands {
		for _, provider := range command.Environment.Providers {
			if provider.Type == "command" {
				for _, executable := range manifest.CommandExecutables(provider.Argv) {
					add(executable)
				}
			}
			if provider.Type == "direnv" {
				add("direnv")
			}
		}
	}
	return result
}

func processComposeVersion(ctx context.Context, executable string) (string, error) {
	versionContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := subprocess.Combined(exec.CommandContext(versionContext, executable, "version"))
	if err != nil {
		return "", fmt.Errorf("%w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Version:") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Version:")), nil
		}
	}
	return "", fmt.Errorf("version line was missing")
}

func githubCLIAuthentication(ctx context.Context) error {
	authContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := subprocess.Run(exec.CommandContext(authContext, "gh", "auth", "status", "--hostname", "github.com"))
	return err
}

func lifecycleExecutable(root string, command manifest.LifecycleCommand) error {
	executable := command.Run.Argv[0]
	if !strings.ContainsRune(executable, os.PathSeparator) {
		_, err := exec.LookPath(executable)
		return err
	}
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(root, command.WorkingDirectory, executable)
	}
	info, err := os.Stat(executable)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("not executable")
	}
	return nil
}
