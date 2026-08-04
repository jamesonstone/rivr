//go:build darwin || linux

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
)

func TestExecuteUsesExactArgvDirectoryEnvironmentAndRedactedLog(t *testing.T) {
	t.Parallel()
	workspaceRoot := t.TempDir()
	workspaceRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory := filepath.Join(workspaceRoot, "tools")
	if err := os.MkdirAll(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(workingDirectory, "record")
	content := "#!/bin/sh\nprintf 'pwd=%s\\narg=%s\\nvalue=%s\\n' \"$PWD\" \"$1\" \"$API_TOKEN\"\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	command := manifest.LifecycleCommand{
		Name: "prepare", WorkingDirectory: "tools", Timeout: manifest.Duration{Duration: time.Second},
		Run:         manifest.Command{Argv: []string{"./record", "argument with spaces"}},
		Environment: manifest.Environment{Values: map[string]string{"API_TOKEN": "secret-value"}},
	}
	outcome, err := Execute(context.Background(), layout, "generation", workspaceRoot, "before_up", 0, command)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "succeeded" || outcome.Log == "" {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
	logContent, err := os.ReadFile(filepath.Join(layout.ProjectDir, filepath.FromSlash(outcome.Log)))
	if err != nil {
		t.Fatal(err)
	}
	log := string(logContent)
	if !strings.Contains(log, "pwd="+workingDirectory) || !strings.Contains(log, "arg=argument with spaces") {
		t.Fatalf("exact execution details missing: %s", log)
	}
	if strings.Contains(log, "secret-value") || !strings.Contains(log, "value=[REDACTED]") {
		t.Fatalf("lifecycle log was not redacted: %s", log)
	}
	info, err := os.Stat(filepath.Join(layout.ProjectDir, filepath.FromSlash(outcome.Log)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lifecycle log is not private: mode=%v", info.Mode())
	}
}

func TestExecuteTimesOutProcessGroup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "slow")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	command := manifest.LifecycleCommand{
		Name: "slow", WorkingDirectory: ".", Timeout: manifest.Duration{Duration: 50 * time.Millisecond},
		Run: manifest.Command{Argv: []string{"./slow"}},
	}
	started := time.Now()
	outcome, err := Execute(context.Background(), layout, "generation", root, "before_up", 0, command)
	if err == nil || outcome.Status != "timed-out" || outcome.ExitCode != 124 {
		t.Fatalf("expected timeout outcome, got %#v err=%v", outcome, err)
	}
	if errs.Code(err) != errs.ExitFailure {
		t.Fatalf("timeout error code = %d", errs.Code(err))
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("timed-out process group took too long to stop: %s", time.Since(started))
	}
}

func TestExecuteMapsParentCancellationToInterrupted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "slow")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	command := manifest.LifecycleCommand{
		Name: "slow", WorkingDirectory: ".", Timeout: manifest.Duration{Duration: 5 * time.Second},
		Run: manifest.Command{Argv: []string{"./slow"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	outcome, err := Execute(ctx, layout, "generation", root, "before_up", 0, command)
	if err == nil || outcome.Status != "interrupted" || errs.Code(err) != errs.ExitInterrupted {
		t.Fatalf("expected interrupted outcome, got %#v code=%d err=%v", outcome, errs.Code(err), err)
	}
}
