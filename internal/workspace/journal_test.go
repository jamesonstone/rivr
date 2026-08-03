package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/state"
)

func TestJournalRoundTripIsPrivateAndAtomic(t *testing.T) {
	t.Parallel()
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	journal := NewJournal("example-k7m4q2", "generation", "manifest-hash", "lifecycle-hash", t.TempDir(), true)
	journal.Record(CommandOutcome{Phase: "before_up", Name: "prepare", Status: "succeeded"})
	if err := WriteJournal(layout, journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadJournal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateStarting || !loaded.TeardownRequired || len(loaded.CompletedBefore) != 1 {
		t.Fatalf("journal changed on round trip: %#v", loaded)
	}
	info, err := os.Stat(filepath.Join(layout.ProjectDir, "lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode = %o", info.Mode().Perm())
	}
}

func TestJournalRejectsSymlink(t *testing.T) {
	t.Parallel()
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(layout.ProjectDir, "lifecycle.json")); err != nil {
		t.Fatal(err)
	}
	_, err = ReadJournal(layout)
	if err == nil || !strings.Contains(err.Error(), "private regular file") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestProjectLifecycleLockIsExclusiveAndContextBound(t *testing.T) {
	t.Parallel()
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(context.Background(), layout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, layout); err == nil || !strings.Contains(err.Error(), "lifecycle lock") {
		t.Fatalf("expected exclusive lock failure, got %v", err)
	}
}
