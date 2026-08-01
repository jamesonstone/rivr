//go:build darwin || linux

package session

import (
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/state"
)

func TestExclusiveGenerationScopedSessionLock(t *testing.T) {
	t.Parallel()
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(layout, "generation-one", "api", "tab-one")
	if err != nil {
		t.Fatal(err)
	}
	if registration, live := Active(layout, "generation-one", "api"); !live || registration.TabID != "tab-one" {
		t.Fatalf("active registration was not visible: %#v live=%v", registration, live)
	}
	if _, err := Acquire(layout, "generation-one", "api", "tab-two"); err == nil || !strings.Contains(err.Error(), "already has an owning session") {
		t.Fatalf("expected duplicate ownership rejection, got %v", err)
	}
	secondGeneration, err := Acquire(layout, "generation-two", "api", "tab-two")
	if err != nil {
		t.Fatalf("generation-scoped lock collided: %v", err)
	}
	if err := secondGeneration.Release(); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
}
