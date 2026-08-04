//go:build darwin || linux

package lifecycle

import (
	"context"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
	"github.com/jamesonstone/rungrid/internal/workspace"
)

func runBeforeUp(
	ctx context.Context,
	layout state.Layout,
	journal *workspace.Journal,
	configuration *manifest.Manifest,
) error {
	for index, command := range configuration.Lifecycle.BeforeUp {
		outcome, err := workspace.Execute(
			ctx,
			layout,
			journal.GenerationID,
			journal.WorkspaceRoot,
			"before_up",
			index,
			command,
		)
		journal.Record(outcome)
		if writeErr := workspace.WriteJournal(layout, *journal); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func runAfterDown(
	ctx context.Context,
	layout state.Layout,
	journal *workspace.Journal,
	configuration *manifest.Manifest,
) error {
	var failures []string
	for index, command := range configuration.Lifecycle.AfterDown {
		outcome, err := workspace.Execute(
			ctx,
			layout,
			journal.GenerationID,
			journal.WorkspaceRoot,
			"after_down",
			index,
			command,
		)
		journal.Record(outcome)
		if writeErr := workspace.WriteJournal(layout, *journal); writeErr != nil {
			failures = append(failures, command.Name+": persist outcome failed")
		} else if err != nil {
			failures = append(failures, command.Name+": "+outcome.Status)
		}
	}
	if len(failures) > 0 {
		return errs.New(errs.ExitPartial, "RG1130", "workspace teardown failed:\n  - "+strings.Join(failures, "\n  - "))
	}
	return nil
}

func attachRuntime(journal *workspace.Journal, runtimeState supervisor.Runtime) {
	journal.Runtime = &workspace.RuntimeIdentity{
		PID: runtimeState.PID, ProcessIdentity: runtimeState.ProcessIdentity,
		Socket: runtimeState.Socket, SocketDevice: runtimeState.SocketDevice,
		SocketInode: runtimeState.SocketInode,
	}
}
