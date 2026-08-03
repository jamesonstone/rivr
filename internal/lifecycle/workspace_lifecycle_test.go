//go:build darwin || linux

package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/workspace"
)

func TestDownRunsAllTeardownWithoutRuntimeAndRetriesCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExecutable(t, filepath.Join(root, "hook"), `#!/bin/sh
printf '%s\n' "$1" >> lifecycle-events
if [ "$1" = "fail" ] && [ ! -f allow-failure ]; then exit 9; fi
`)
	commands := []manifest.LifecycleCommand{
		lifecycleCommand("first", "./hook", "first"),
		lifecycleCommand("failing", "./hook", "fail"),
		lifecycleCommand("last", "./hook", "last"),
	}
	layout, journal := lifecycleFixture(t, root, nil, commands)
	if err := DownProject(context.Background(), layout); err == nil {
		t.Fatal("expected partial teardown failure")
	}
	assertLines(t, filepath.Join(root, "lifecycle-events"), []string{"first", "fail", "last"})
	journal, err := workspace.ReadJournal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != workspace.StateCleanup || !journal.TeardownRequired || len(journal.Outcomes) != 3 {
		t.Fatalf("cleanup obligation was not retained: %#v", journal)
	}
	status, err := InspectStatus(context.Background(), layout)
	if err != nil {
		t.Fatal(err)
	}
	if status.Runtime != "inactive" || status.Lifecycle == nil || status.Lifecycle.LastFailure == nil ||
		status.Lifecycle.LastFailure.Name != "failing" || status.Lifecycle.HashesCompatible != true {
		t.Fatalf("status omitted cleanup recovery state: %#v", status)
	}
	if err := os.WriteFile(filepath.Join(root, "allow-failure"), []byte("allow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DownProject(context.Background(), layout); err != nil {
		t.Fatal(err)
	}
	assertLines(t, filepath.Join(root, "lifecycle-events"), []string{"first", "fail", "last", "first", "fail", "last"})
	journal, err = workspace.ReadJournal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != workspace.StateInactive || journal.TeardownRequired || journal.CleanupFailure != "" {
		t.Fatalf("successful retry did not close cleanup: %#v", journal)
	}
	if err := DownProject(context.Background(), layout); err != nil {
		t.Fatal(err)
	}
	assertLines(t, filepath.Join(root, "lifecycle-events"), []string{"first", "fail", "last", "first", "fail", "last"})
}

func TestDownFailsClosedWhenRecordedManifestHashChanged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExecutable(t, filepath.Join(root, "hook"), "#!/bin/sh\ntouch should-not-run\n")
	layout, journal := lifecycleFixture(t, root, nil, []manifest.LifecycleCommand{
		lifecycleCommand("cleanup", "./hook"),
	})
	manifestPath := filepath.Join(layout.ProjectDir, "generations", journal.GenerationID, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := DownProject(context.Background(), layout)
	if err == nil || !strings.Contains(err.Error(), "missing or modified") {
		t.Fatalf("expected manifest hash conflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-run")); !os.IsNotExist(err) {
		t.Fatalf("teardown ran after identity failure: %v", err)
	}
}

func TestDownDetectsLifecycleHashMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExecutable(t, filepath.Join(root, "hook"), "#!/bin/sh\ntouch should-not-run\n")
	layout, journal := lifecycleFixture(t, root, nil, []manifest.LifecycleCommand{
		lifecycleCommand("cleanup", "./hook"),
	})
	journal.LifecycleSHA256 = "changed-lifecycle-hash"
	if err := workspace.WriteJournal(layout, journal); err != nil {
		t.Fatal(err)
	}
	err := DownProject(context.Background(), layout)
	if err == nil || !strings.Contains(err.Error(), "lifecycle configuration hash") {
		t.Fatalf("expected lifecycle hash conflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-run")); !os.IsNotExist(err) {
		t.Fatalf("teardown ran after lifecycle hash failure: %v", err)
	}
}

func TestUninstallNeverDiscardsCleanupRequiredState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExecutable(t, filepath.Join(root, "hook"), "#!/bin/sh\nexit 9\n")
	layout, _ := lifecycleFixture(t, root, nil, []manifest.LifecycleCommand{
		lifecycleCommand("cleanup", "./hook"),
	})
	if err := Uninstall(context.Background(), layout, false, false); err == nil {
		t.Fatal("uninstall unexpectedly discarded failed cleanup")
	}
	journal, err := workspace.ReadJournal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != workspace.StateCleanup || !journal.TeardownRequired {
		t.Fatalf("cleanup state was not retained: %#v", journal)
	}
	if err := layout.VerifyMarker(); err != nil {
		t.Fatalf("project ownership state was removed: %v", err)
	}
}

func TestUpRollsBackPrerequisitesWhenSupervisorStartupFails(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	warpRoot := filepath.Join(root, "warp-tabs")
	writeExecutable(t, filepath.Join(root, "hook"), "#!/bin/sh\nprintf '%s\\n' \"$1\" >> lifecycle-events\n")
	writeExecutable(t, filepath.Join(root, "fake-pc"), `#!/bin/sh
if [ "${1:-}" = "version" ]; then
  printf 'Version: v1.120.0\n'
  exit 0
fi
printf 'supervisor\n' >> "$EVENT_FILE"
exit 42
`)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WARP_TAB_CONFIG_DIR", warpRoot)
	t.Setenv("EVENT_FILE", filepath.Join(root, "lifecycle-events"))
	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
runtime:
  process_compose: {executable: fake-pc}
terminal: {mode: warp, open: true}
lifecycle:
  before_up:
    - {name: prepare, run: {argv: [./hook, before]}}
  after_down:
    - {name: cleanup, run: {argv: [./hook, after]}}
services: []
`
	configPath := filepath.Join(root, ".rungrid.yaml")
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manifest.Load(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Up(context.Background(), loaded, UpOptions{
		StateOverride: stateRoot, GeneratorVersion: "test", Open: true,
	})
	if err == nil || !strings.Contains(err.Error(), "start detached Process Compose runtime") {
		t.Fatalf("expected supervisor startup failure, got %v", err)
	}
	assertLines(t, filepath.Join(root, "lifecycle-events"), []string{"before", "supervisor", "after"})
	layout, err := state.NewLayout("example-k7m4q2", stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := workspace.ReadJournal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != workspace.StateInactive || journal.TeardownRequired {
		t.Fatalf("rollback did not close journal: %#v", journal)
	}
	if _, err := os.Stat(filepath.Join(layout.ProjectDir, "runtime.json")); !os.IsNotExist(err) {
		t.Fatalf("failed supervisor left runtime identity: %v", err)
	}
	if entries, err := os.ReadDir(warpRoot); err == nil && len(entries) > 0 {
		t.Fatalf("Warp files were opened after startup failure: %#v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestUpDoesNotStartSupervisorOrOpenWarpAfterPrerequisiteFailure(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	warpRoot := filepath.Join(root, "warp-tabs")
	events := filepath.Join(root, "events")
	writeExecutable(t, filepath.Join(root, "before"), "#!/bin/sh\nprintf 'before\\n' >> \"$EVENT_FILE\"\nexit 7\n")
	writeExecutable(t, filepath.Join(root, "after"), "#!/bin/sh\nprintf 'after\\n' >> \"$EVENT_FILE\"\n")
	writeExecutable(t, filepath.Join(root, "fake-pc"), `#!/bin/sh
if [ "${1:-}" = "version" ]; then printf 'Version: v1.120.0\n'; exit 0; fi
printf 'supervisor\n' >> "$EVENT_FILE"
exit 42
`)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WARP_TAB_CONFIG_DIR", warpRoot)
	t.Setenv("EVENT_FILE", events)
	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
runtime: {process_compose: {executable: fake-pc}}
terminal: {mode: warp, open: true}
lifecycle:
  before_up:
    - {name: prepare, run: {argv: [./before]}}
  after_down:
    - {name: cleanup, run: {argv: [./after]}}
services: []
`
	configPath := filepath.Join(root, ".rungrid.yaml")
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manifest.Load(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Up(context.Background(), loaded, UpOptions{
		StateOverride: stateRoot, GeneratorVersion: "test", Open: true,
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle command prepare failed") {
		t.Fatalf("expected prerequisite failure, got %v", err)
	}
	assertLines(t, events, []string{"before", "after"})
	if entries, err := os.ReadDir(warpRoot); err == nil && len(entries) > 0 {
		t.Fatalf("Warp files exist after prerequisite failure: %#v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
