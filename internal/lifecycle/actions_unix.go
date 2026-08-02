//go:build darwin || linux

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/serviceexec"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
	"github.com/jamesonstone/rungrid/internal/terminalshell"
	"github.com/jamesonstone/rungrid/internal/warp"
)

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
		if service.Source == "compose" {
			if err := serviceexec.ComposeShutdown(service, active.Runtime.WorkspaceRoot, ctx); err != nil {
				failures = append(failures, service.Name+": "+err.Error())
			}
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
