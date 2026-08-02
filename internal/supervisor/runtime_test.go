//go:build darwin || linux

package supervisor

import (
	"path/filepath"
	"testing"
)

func TestReportedSocketMatchesPlatformLsofPaths(t *testing.T) {
	t.Parallel()
	projectDirectory := filepath.Join(string(filepath.Separator), "state", "rungrid", "projects", "example")
	generationDirectory := filepath.Join(projectDirectory, "generations", "generation")
	socket := filepath.Join(projectDirectory, "runtime.sock")

	for _, reported := range []string{
		socket,
		socket + " type=STREAM",
		filepath.Join("..", "..", "runtime.sock"),
		filepath.Join("..", "..", "runtime.sock") + " type=STREAM",
	} {
		if !reportedSocketMatches(reported, socket, generationDirectory) {
			t.Errorf("expected socket path to match: %q", reported)
		}
	}
	for _, reported := range []string{"", filepath.Join(projectDirectory, "other.sock"), "../runtime.sock type=STREAM"} {
		if reportedSocketMatches(reported, socket, generationDirectory) {
			t.Errorf("unexpected socket path match: %q", reported)
		}
	}
}
