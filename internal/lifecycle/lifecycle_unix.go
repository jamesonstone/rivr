//go:build darwin || linux

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/generate"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/serviceexec"
	"github.com/jamesonstone/rungrid/internal/session"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
	"github.com/jamesonstone/rungrid/internal/terminalshell"
	"github.com/jamesonstone/rungrid/internal/warp"
	"gopkg.in/yaml.v3"
)

type Active struct {
	Layout   state.Layout
	Runtime  supervisor.Runtime
	Manifest *manifest.Manifest
}

type UpOptions struct {
	StateOverride    string
	GeneratorVersion string
	Headless         bool
	Open             bool
	Requested        []string
}

type UpResult struct {
	Generation string `json:"generation"`
	RuntimePID int    `json:"runtime_pid"`
	Socket     string `json:"socket"`
	Reused     bool   `json:"reused"`
	OpenedWarp bool   `json:"opened_warp"`
}

type ServiceStatus struct {
	Name          string `json:"name"`
	Source        string `json:"source"`
	Activation    string `json:"activation"`
	Status        string `json:"status"`
	Health        string `json:"health,omitempty"`
	PID           int    `json:"pid,omitempty"`
	ExitCode      int    `json:"exit_code,omitempty"`
	SessionOwned  bool   `json:"session_owned"`
	TabRegistered bool   `json:"tab_registered"`
}

func LoadActive(ctx context.Context, projectID, stateOverride string) (Active, error) {
	if projectID == "" {
		return Active{}, errs.New(errs.ExitUsage, "RG1101", "project id is required to select active state")
	}
	layout, err := state.NewLayout(projectID, stateOverride)
	if err != nil {
		return Active{}, err
	}
	runtimeState, err := supervisor.Read(layout)
	if err != nil {
		if os.IsNotExist(err) {
			return Active{}, errs.New(errs.ExitConflict, "RG1102", "no active Rungrid runtime")
		}
		return Active{}, err
	}
	if err := supervisor.Verify(ctx, layout, runtimeState); err != nil {
		return Active{}, err
	}
	manifestPath := filepath.Join(layout.ProjectDir, "generations", runtimeState.GenerationID, "manifest.yaml")
	generatedManifest, err := manifest.LoadGenerated(manifestPath, runtimeState.WorkspaceRoot)
	if err != nil {
		return Active{}, err
	}
	return Active{Layout: layout, Runtime: runtimeState, Manifest: generatedManifest}, nil
}

func Up(ctx context.Context, loaded *manifest.Loaded, options UpOptions) (UpResult, error) {
	effective := loaded
	if options.Headless && loaded.Manifest.Terminal.Mode != "headless" {
		copyLoaded := *loaded
		copyLoaded.Manifest = loaded.Manifest
		copyLoaded.Manifest.Terminal.Mode = "headless"
		copyLoaded.Manifest.Terminal.Open = manifest.Bool(false)
		content, err := yaml.Marshal(copyLoaded.Manifest)
		if err != nil {
			return UpResult{}, errs.Wrap(errs.ExitFailure, "RG1126", "encode headless manifest override", err)
		}
		copyLoaded.MergedYAML = content
		effective = &copyLoaded
	}
	generated, err := generate.Run(effective, options.StateOverride, options.GeneratorVersion, false)
	if err != nil {
		return UpResult{}, err
	}
	for i := range effective.Manifest.Services {
		service := &effective.Manifest.Services[i]
		if service.Source != "external" {
			continue
		}
		waitContext, cancel := context.WithTimeout(ctx, effective.Manifest.Runtime.StartupTimeout.Duration)
		err := serviceexec.WaitExternal(waitContext, effective.Root, service)
		cancel()
		if err != nil {
			return UpResult{}, err
		}
	}
	pcExecutable, err := exec.LookPath(effective.Manifest.Runtime.ProcessCompose.Executable)
	if err != nil {
		return UpResult{}, errs.Wrap(errs.ExitDependency, "RG1103", "resolve Process Compose executable", err)
	}
	pcVersion, err := processcompose.Version(ctx, pcExecutable)
	if err != nil {
		return UpResult{}, err
	}
	rungridExecutable, err := processcompose.ExecutablePath()
	if err != nil {
		return UpResult{}, errs.Wrap(errs.ExitFailure, "RG1104", "resolve Rungrid executable", err)
	}
	layout, err := state.NewLayout(effective.Manifest.Project.ID, options.StateOverride)
	if err != nil {
		return UpResult{}, err
	}
	runtimeState, reused, err := supervisor.Start(ctx, supervisor.StartOptions{
		Layout: layout, GenerationID: generated.Plan.GenerationID, WorkspaceRoot: effective.Root,
		ProcessCompose: pcExecutable, ProcessComposeVersion: pcVersion, RungridExecutable: rungridExecutable,
		StartupTimeout: effective.Manifest.Runtime.StartupTimeout.Duration,
	})
	if err != nil {
		return UpResult{}, err
	}
	client := supervisor.Client(layout, runtimeState)
	readyContext, cancel := context.WithTimeout(ctx, effective.Manifest.Runtime.StartupTimeout.Duration)
	_, readyErr := client.Run(readyContext, "project", "is-ready", "--wait")
	cancel()
	if readyErr != nil {
		return UpResult{}, errs.Wrap(errs.ExitNotReady, "RG1105", "workspace-owned services did not become ready", readyErr)
	}

	opened := false
	shouldOpen := options.Open && !options.Headless && effective.Manifest.Terminal.Mode == "warp"
	if shouldOpen {
		record, installErr := warp.Install(layout, &effective.Manifest, generated.Plan.GenerationID, rungridExecutable)
		if installErr != nil {
			return UpResult{}, installErr
		}
		if openErr := warp.Open(ctx, record, ""); openErr != nil {
			return UpResult{}, openErr
		}
		opened = true
	}
	for _, name := range options.Requested {
		service, exists := manifest.FindService(&effective.Manifest, name)
		if !exists {
			return UpResult{}, errs.New(errs.ExitUsage, "RG1106", "unknown requested service: "+name)
		}
		if service.Activation == "tab" && !shouldOpen {
			return UpResult{}, errs.New(errs.ExitUsage, "RG1107", "requested tab service requires Warp opening or a separate rungrid session")
		}
		waitContext, waitCancel := context.WithTimeout(ctx, effective.Manifest.Runtime.StartupTimeout.Duration)
		err := waitForService(waitContext, client, layout, runtimeState.GenerationID, service)
		waitCancel()
		if err != nil {
			return UpResult{}, err
		}
	}
	return UpResult{Generation: runtimeState.GenerationID, RuntimePID: runtimeState.PID, Socket: runtimeState.Socket, Reused: reused, OpenedWarp: opened}, nil
}

func Open(ctx context.Context, active Active, service string) error {
	if active.Manifest.Terminal.Mode != "warp" {
		return errs.New(errs.ExitUsage, "RG1108", "workspace is configured for headless mode")
	}
	executable, err := processcompose.ExecutablePath()
	if err != nil {
		return err
	}
	record, err := warp.Install(active.Layout, active.Manifest, active.Runtime.GenerationID, executable)
	if err != nil {
		return err
	}
	return warp.Open(ctx, record, service)
}

func Start(ctx context.Context, active Active, serviceName string) (string, error) {
	service, exists := manifest.FindService(active.Manifest, serviceName)
	if !exists {
		return "", errs.New(errs.ExitUsage, "RG1109", "unknown service: "+serviceName)
	}
	if service.Source == "external" {
		waitContext, cancel := context.WithTimeout(ctx, active.Manifest.Runtime.StartupTimeout.Duration)
		defer cancel()
		if err := serviceexec.WaitExternal(waitContext, active.Runtime.WorkspaceRoot, service); err != nil {
			return "", err
		}
		return "external service is ready; lifecycle remains external", nil
	}
	if service.Activation == "tab" {
		if _, live := terminalshell.ActiveTab(active.Layout, active.Runtime.GenerationID, serviceName); live {
			return fmt.Sprintf("tab already exists; run %s there", formatArgv(service.Terminal.TriggerArgv)), nil
		}
		if active.Manifest.Terminal.Mode != "warp" {
			return "", errs.New(errs.ExitUsage, "RG1110", "headless tab-owned services start with rungrid session "+serviceName)
		}
		if err := Open(ctx, active, serviceName); err != nil {
			return "", err
		}
		return "opened the absent service tab; its managed shell is acquiring ownership", nil
	}
	client := supervisor.Client(active.Layout, active.Runtime)
	if err := client.Start(ctx, serviceName); err != nil {
		return "", err
	}
	waitContext, cancel := context.WithTimeout(ctx, active.Manifest.Runtime.StartupTimeout.Duration)
	defer cancel()
	if err := waitForService(waitContext, client, active.Layout, active.Runtime.GenerationID, service); err != nil {
		return "", err
	}
	return "service started", nil
}

func Stop(ctx context.Context, active Active, serviceName string) error {
	service, exists := manifest.FindService(active.Manifest, serviceName)
	if !exists {
		return errs.New(errs.ExitUsage, "RG1111", "unknown service: "+serviceName)
	}
	if service.Source == "external" {
		return errs.New(errs.ExitUsage, "RG1112", "Rungrid does not own external service lifecycle")
	}
	return supervisor.Client(active.Layout, active.Runtime).Stop(ctx, serviceName)
}

func Down(ctx context.Context, active Active) error {
	markerRelative := filepath.Join("locks", "down-"+active.Runtime.GenerationID+".json")
	marker := []byte(fmt.Sprintf("{\"api_version\":\"rungrid/output/v1\",\"project_id\":%q,\"generation_id\":%q}\n", active.Layout.ProjectID, active.Runtime.GenerationID))
	if err := state.WriteFileAtomic(active.Layout.ProjectDir, markerRelative, marker, 0o600); err != nil {
		return err
	}
	markerPath := filepath.Join(active.Layout.ProjectDir, markerRelative)
	defer func() { _ = os.Remove(markerPath) }()

	client := supervisor.Client(active.Layout, active.Runtime)
	var failures []string
	for i := len(active.Manifest.Services) - 1; i >= 0; i-- {
		service := &active.Manifest.Services[i]
		if service.Source == "external" {
			continue
		}
		if current, err := client.Get(ctx, service.Name); err == nil && !shouldStop(current.Status) {
			continue
		}
		if err := client.Stop(ctx, service.Name); err != nil && !isAlreadyStopped(err) {
			failures = append(failures, service.Name+": "+err.Error())
		}
	}
	for i := len(active.Manifest.Services) - 1; i >= 0; i-- {
		service := &active.Manifest.Services[i]
		if service.Source != "compose" {
			continue
		}
		if err := serviceexec.ComposeShutdown(service, active.Runtime.WorkspaceRoot, ctx); err != nil {
			failures = append(failures, service.Name+": "+err.Error())
		}
	}
	if err := supervisor.Stop(ctx, active.Layout, active.Runtime); err != nil {
		failures = append(failures, "runtime: "+err.Error())
	}
	if len(failures) > 0 {
		return errs.New(errs.ExitPartial, "RG1113", "partial workspace shutdown:\n  - "+strings.Join(failures, "\n  - "))
	}
	return nil
}

func Logs(ctx context.Context, active Active, services []string, follow bool, tail int, raw bool, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(services) == 0 {
		for _, service := range active.Manifest.Services {
			if service.Source != "external" {
				services = append(services, service.Name)
			}
		}
	}
	for _, name := range services {
		service, exists := manifest.FindService(active.Manifest, name)
		if !exists || service.Source == "external" {
			return errs.New(errs.ExitUsage, "RG1114", "logs require a managed service: "+name)
		}
	}
	command := supervisor.Client(active.Layout, active.Runtime).LogsCommand(ctx, services, follow, tail, raw, stdin, stdout, stderr)
	if err := command.Run(); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG1115", "read Process Compose logs", err)
	}
	return nil
}

func Attach(ctx context.Context, active Active, readOnly bool, stdin io.Reader, stdout, stderr io.Writer) error {
	command := supervisor.Client(active.Layout, active.Runtime).AttachCommand(ctx, readOnly, stdin, stdout, stderr)
	if err := command.Run(); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG1116", "attach Process Compose TUI", err)
	}
	return nil
}

func Status(ctx context.Context, active Active) ([]ServiceStatus, json.RawMessage, error) {
	states, raw, err := supervisor.Client(active.Layout, active.Runtime).List(ctx)
	if err != nil {
		return nil, nil, err
	}
	byName := make(map[string]processcompose.ProcessState, len(states))
	for _, processState := range states {
		byName[processState.Name] = processState
	}
	result := make([]ServiceStatus, 0, len(active.Manifest.Services))
	for i := range active.Manifest.Services {
		service := &active.Manifest.Services[i]
		item := ServiceStatus{Name: service.Name, Source: service.Source, Activation: service.Activation, Status: "external"}
		if processState, exists := byName[service.Name]; exists {
			item.Status = processState.Status
			item.Health = processState.Health
			item.PID = processState.PID
			item.ExitCode = processState.ExitCode
		}
		_, item.SessionOwned = session.Active(active.Layout, active.Runtime.GenerationID, service.Name)
		_, item.TabRegistered = terminalshell.ActiveTab(active.Layout, active.Runtime.GenerationID, service.Name)
		result = append(result, item)
	}
	return result, raw, nil
}

func Uninstall(ctx context.Context, layout state.Layout, keepLogs, keepConfig bool) error {
	if err := layout.VerifyMarker(); err != nil {
		return err
	}
	if runtimeState, err := supervisor.Read(layout); err == nil {
		if err := supervisor.Verify(ctx, layout, runtimeState); err != nil {
			return err
		}
		manifestPath := filepath.Join(layout.ProjectDir, "generations", runtimeState.GenerationID, "manifest.yaml")
		generatedManifest, err := manifest.LoadGenerated(manifestPath, runtimeState.WorkspaceRoot)
		if err != nil {
			return err
		}
		if err := Down(ctx, Active{Layout: layout, Runtime: runtimeState, Manifest: generatedManifest}); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := warp.Uninstall(layout); err != nil {
		return err
	}
	expectedParent := filepath.Join(layout.StateRoot, "rungrid", "projects")
	if filepath.Dir(layout.ProjectDir) != expectedParent || filepath.Base(layout.ProjectDir) != layout.ProjectID {
		return errs.New(errs.ExitConflict, "RG1117", "refusing to uninstall outside the exact project state directory")
	}
	if keepLogs || keepConfig {
		return uninstallPreserving(layout, keepLogs, keepConfig)
	}
	if err := os.RemoveAll(layout.ProjectDir); err != nil {
		return errs.Wrap(errs.ExitPartial, "RG1118", "remove owned project state", err)
	}
	return nil
}

func uninstallPreserving(layout state.Layout, keepLogs, keepConfig bool) error {
	if keepLogs && !keepConfig {
		generationsDirectory := filepath.Join(layout.ProjectDir, "generations")
		entries, err := os.ReadDir(generationsDirectory)
		if err != nil && !os.IsNotExist(err) {
			return errs.Wrap(errs.ExitPartial, "RG1120", "inspect generation logs for preservation", err)
		}
		preservedLogs := filepath.Join(layout.ProjectDir, "preserved-logs")
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			source := filepath.Join(generationsDirectory, entry.Name(), "logs")
			if info, statErr := os.Lstat(source); statErr == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errs.New(errs.ExitConflict, "RG1121", "generation log path is not a real directory")
				}
				if err := os.MkdirAll(preservedLogs, 0o700); err != nil {
					return errs.Wrap(errs.ExitPartial, "RG1122", "create preserved log directory", err)
				}
				if err := os.Rename(source, filepath.Join(preservedLogs, entry.Name())); err != nil {
					return errs.Wrap(errs.ExitPartial, "RG1123", "preserve generation logs", err)
				}
			} else if !os.IsNotExist(statErr) {
				return errs.Wrap(errs.ExitPartial, "RG1124", "inspect generation log path", statErr)
			}
		}
	}
	remove := []string{"sessions", "tabs", "locks", "terminal-install.json", "runtime.json", "runtime.sock"}
	if !keepConfig {
		remove = append(remove, "generations", "current")
	}
	if !keepLogs {
		remove = append(remove, "process-compose.log", "client.log", "preserved-logs")
	}
	for _, name := range remove {
		target := filepath.Join(layout.ProjectDir, name)
		if err := os.RemoveAll(target); err != nil {
			return errs.Wrap(errs.ExitPartial, "RG1125", "remove owned project state component", err)
		}
	}
	return nil
}

func waitForService(ctx context.Context, client processcompose.Client, layout state.Layout, generationID string, service *manifest.Service) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if service.Activation == "tab" {
			if _, live := terminalshell.ActiveTab(layout, generationID, service.Name); live {
				return nil
			}
		}
		stateValue, err := client.Get(ctx, service.Name)
		if err == nil {
			normalized := strings.ToLower(stateValue.Status)
			if strings.Contains(normalized, "running") || strings.Contains(normalized, "healthy") {
				if service.Health == nil || strings.Contains(strings.ToLower(stateValue.Health), "healthy") {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return errs.Wrap(errs.ExitNotReady, "RG1119", "service did not report ready state: "+service.Name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func isAlreadyStopped(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not running") || strings.Contains(message, "disabled") || strings.Contains(message, "already stopped")
}

func shouldStop(status string) bool {
	normalized := strings.ToLower(status)
	return strings.Contains(normalized, "running") || strings.Contains(normalized, "launch") || strings.Contains(normalized, "pending") || strings.Contains(normalized, "waiting")
}

func formatArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, value := range argv {
		if strings.ContainsAny(value, " \t\n\"'") {
			parts[i] = fmt.Sprintf("%q", value)
		} else {
			parts[i] = value
		}
	}
	return strings.Join(parts, " ")
}

func SessionActive(layout state.Layout, generationID, service string) bool {
	_, live := session.Active(layout, generationID, service)
	return live
}
