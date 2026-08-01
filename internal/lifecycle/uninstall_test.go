//go:build darwin || linux

package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesonstone/rungrid/internal/state"
)

func TestUninstallPreservesRequestedLogsWithoutGeneratedConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	layout, err := state.NewLayout("example-k7m4q2", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	generation := filepath.Join(layout.ProjectDir, "generations", "generation")
	if err := os.MkdirAll(filepath.Join(generation, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generation, "manifest.yaml"), []byte("owned config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generation, "logs", "api.log"), []byte("owned log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.ProjectDir, "process-compose.log"), []byte("runtime log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), layout, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(layout.ProjectDir, "preserved-logs", "generation", "api.log")); err != nil {
		t.Fatalf("generation log was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.ProjectDir, "process-compose.log")); err != nil {
		t.Fatalf("runtime log was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.ProjectDir, "generations")); !os.IsNotExist(err) {
		t.Fatalf("generated configuration remains: %v", err)
	}
	if err := layout.VerifyMarker(); err != nil {
		t.Fatalf("preserved state lost ownership marker: %v", err)
	}
}

func TestUninstallRemovesOnlyExactProjectDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	layout, err := state.NewLayout("example-k7m4q2", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "unrelated.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), layout, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.ProjectDir); !os.IsNotExist(err) {
		t.Fatalf("project state remains: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("uninstall changed unrelated state: %v", err)
	}
}
