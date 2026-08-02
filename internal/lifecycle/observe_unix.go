//go:build darwin || linux

package lifecycle

import (
	"context"
	"encoding/json"
	"io"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/session"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
	"github.com/jamesonstone/rungrid/internal/terminalshell"
)

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

func SessionActive(layout state.Layout, generationID, service string) bool {
	_, live := session.Active(layout, generationID, service)
	return live
}
