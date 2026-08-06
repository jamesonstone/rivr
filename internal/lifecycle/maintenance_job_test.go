//go:build darwin || linux

package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/maintenance"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
)

func TestStartMaintenanceJobUsesAuthorizedProcessComposeProcess(t *testing.T) {
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	argumentsPath := filepath.Join(t.TempDir(), "arguments")
	executable := filepath.Join(t.TempDir(), "process-compose")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_ARGUMENTS\"\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_ARGUMENTS", argumentsPath)
	active := Active{Layout: layout, Runtime: supervisor.Runtime{
		ProjectID: "example-k7m4q2", GenerationID: "0123456789abcdefabcd",
		ProcessCompose: executable, WorkspaceRoot: t.TempDir(),
	}}
	workerDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for {
			request, claim, claimErr := maintenance.ClaimRequest(layout, "0123456789abcdefabcd", maintenance.OperationSync)
			if claimErr == nil {
				defer maintenance.CleanupClaim(claim)
				workerDone <- maintenance.WriteJobResult(layout, request, maintenance.SyncReport{Operation: maintenance.OperationSync}, nil)
				return
			}
			select {
			case <-ctx.Done():
				workerDone <- ctx.Err()
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	result, err := StartMaintenanceJob(context.Background(), active, maintenance.OperationSync, []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-workerDone; err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("unexpected result: %#v", result)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(arguments), "process\nstart\n"+maintenance.SyncProcessName+"\n") {
		t.Fatalf("unexpected Process Compose arguments: %q", arguments)
	}
}

func TestMaintenanceWorktreeContainsDeclaredRepositorySubdirectory(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace", "api")
	if !withinMaintenanceWorktree(root, filepath.Join(root, "services", "server")) {
		t.Fatal("declared repository subdirectory was not mapped to its Git worktree")
	}
	if withinMaintenanceWorktree(root, filepath.Join(string(filepath.Separator), "workspace", "web")) {
		t.Fatal("sibling repository was mapped to the default worktree")
	}
}
