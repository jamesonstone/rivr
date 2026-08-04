package versions

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/serviceexec"
	"github.com/jamesonstone/rungrid/internal/subprocess"
	"github.com/jamesonstone/rungrid/internal/supervisor"
)

type Snapshot struct {
	CapturedAt string           `json:"captured_at"`
	Runtime    string           `json:"runtime"`
	Generation string           `json:"generation"`
	Services   []ServiceVersion `json:"services"`
}

type ServiceVersion struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Health   string `json:"health,omitempty"`
	PID      int    `json:"pid,omitempty"`
	Ports    []int  `json:"ports"`
	Branch   string `json:"branch,omitempty"`
	Commit   string `json:"commit,omitempty"`
	GitState string `json:"git_state"`
	Worktree string `json:"worktree,omitempty"`
}

func Capture(ctx context.Context, m *manifest.Manifest, runtimeState supervisor.Runtime, client processcompose.Client) Snapshot {
	result := Snapshot{
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Runtime:    "running",
		Generation: runtimeState.GenerationID,
	}
	states, _, err := client.List(ctx)
	if err != nil {
		result.Runtime = "unavailable"
	}
	byName := make(map[string]processcompose.ProcessState, len(states))
	for _, state := range states {
		byName[state.Name] = state
	}
	for i := range m.Services {
		service := &m.Services[i]
		if service.Terminal.IncludeInVersions != nil && !*service.Terminal.IncludeInVersions {
			continue
		}
		item := ServiceVersion{Name: service.Name, State: "unknown", GitState: "unavailable", Ports: append([]int(nil), service.Ports...)}
		if service.Source == "external" {
			if serviceexec.CheckExternal(ctx, runtimeState.WorkspaceRoot, service) == nil {
				item.State = "external-ready"
				item.Health = "healthy"
			} else {
				item.State = "external-unavailable"
				item.Health = "unhealthy"
			}
		} else if state, exists := byName[service.Name]; exists {
			item.State = state.Status
			item.Health = state.Health
			item.PID = state.PID
			if state.PID > 0 {
				if ports := listeningPorts(ctx, state.PID); len(ports) > 0 {
					item.Ports = ports
				}
			}
		}
		item.Branch, item.Commit, item.GitState, item.Worktree = gitVersion(ctx, filepath.Join(runtimeState.WorkspaceRoot, service.WorkingDirectory))
		result.Services = append(result.Services, item)
	}
	return result
}

func WriteHuman(w io.Writer, snapshot Snapshot) {
	_, _ = fmt.Fprintf(w, "Rungrid Versions  %s  generation %s\n\n", snapshot.CapturedAt, snapshot.Generation)
	_, _ = fmt.Fprintf(w, "%-18s %-18s %-9s %-7s %-12s %-18s %-10s\n", "SERVICE", "STATE", "HEALTH", "PID", "PORTS", "BRANCH@COMMIT", "GIT")
	for _, service := range snapshot.Services {
		ports := "-"
		if len(service.Ports) > 0 {
			parts := make([]string, len(service.Ports))
			for i, port := range service.Ports {
				parts[i] = strconv.Itoa(port)
			}
			ports = strings.Join(parts, ",")
		}
		pid := "-"
		if service.PID > 0 {
			pid = strconv.Itoa(service.PID)
		}
		version := "-"
		if service.Branch != "" || service.Commit != "" {
			version = service.Branch + "@" + service.Commit
		}
		health := service.Health
		if health == "" {
			health = "-"
		}
		_, _ = fmt.Fprintf(w, "%-18s %-18s %-9s %-7s %-12s %-18s %-10s\n", service.Name, service.State, health, pid, ports, version, service.GitState)
	}
}

func listeningPorts(ctx context.Context, pid int) []int {
	capture, err := subprocess.Run(exec.CommandContext(ctx, "lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN", "-F", "n"))
	if err != nil {
		return nil
	}
	seen := map[int]bool{}
	for _, line := range strings.Split(string(capture.Stdout), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		address := strings.TrimPrefix(line, "n")
		index := strings.LastIndexByte(address, ':')
		if index < 0 {
			continue
		}
		portText := address[index+1:]
		if end := strings.IndexByte(portText, '-'); end >= 0 {
			portText = portText[:end]
		}
		if port, parseErr := strconv.Atoi(portText); parseErr == nil && port > 0 && port <= 65535 {
			seen[port] = true
		}
	}
	result := make([]int, 0, len(seen))
	for port := range seen {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

func gitVersion(ctx context.Context, directory string) (branch, commit, gitState, worktree string) {
	branch = runGit(ctx, directory, "branch", "--show-current")
	commit = runGit(ctx, directory, "rev-parse", "--short", "HEAD")
	if branch == "" && commit == "" {
		return "", "", "unavailable", ""
	}
	statusCommand := exec.CommandContext(ctx, "git", "-C", directory, "status", "--porcelain", "--untracked-files=normal")
	statusResult, err := subprocess.Run(statusCommand)
	if err != nil {
		gitState = "unavailable"
	} else if len(statusResult.Stdout) == 0 {
		gitState = "clean"
	} else {
		gitState = "dirty"
	}
	root := runGit(ctx, directory, "rev-parse", "--show-toplevel")
	if root != "" {
		worktree = filepath.Base(root)
	}
	return branch, commit, gitState, worktree
}

func runGit(ctx context.Context, directory string, arguments ...string) string {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, arguments...)...)
	result, err := subprocess.Run(command)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(result.Stdout))
}
