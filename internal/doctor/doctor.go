package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/state"
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

	required := requiredExecutables(&loaded.Manifest)
	for _, executable := range required {
		if _, err := exec.LookPath(executable); err != nil {
			add(Check{Name: "executable:" + executable, Status: "error", Summary: "required executable was not found"})
		} else {
			add(Check{Name: "executable:" + executable, Status: "ok", Summary: "required executable is available"})
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
		if service.Run != nil && len(service.Run.Argv) > 0 {
			add(service.Run.Argv[0])
		}
		if service.Compose != nil && len(service.Compose.UpArgv) > 0 {
			add(service.Compose.UpArgv[0])
		}
		if service.External != nil && service.External.Command != nil && len(service.External.Command.Argv) > 0 {
			add(service.External.Command.Argv[0])
		}
		if service.Health != nil && service.Health.Command != nil && len(service.Health.Command.Argv) > 0 {
			add(service.Health.Command.Argv[0])
		}
		for _, provider := range service.Environment.Providers {
			if provider.Type == "command" && len(provider.Argv) > 0 {
				add(provider.Argv[0])
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
	output, err := exec.CommandContext(versionContext, executable, "version").CombinedOutput()
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
