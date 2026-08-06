package agentinstructions

import (
	"strings"
	"testing"
)

func TestBuildDefaultsToCurrentProject(t *testing.T) {
	t.Parallel()
	document := Build(".rungrid.yaml", nil)
	if len(document.Inputs.ProjectPaths) != 1 || document.Inputs.ProjectPaths[0] != "." {
		t.Fatalf("unexpected default project paths: %#v", document.Inputs.ProjectPaths)
	}
	for _, expected := range []string{
		"single active service inventory",
		"lifecycle.before_up",
		"terminal.trigger_argv",
		"rungrid init",
		"rungrid generate --check",
		"rungrid sync --dry-run",
		"rungrid worktrees prune --dry-run",
		"Do not add them to Rungrid source",
	} {
		if !strings.Contains(document.Instructions, expected) {
			t.Errorf("instructions missing %q", expected)
		}
	}
}

func TestBuildTreatsProjectPathsAsJSONData(t *testing.T) {
	t.Parallel()
	document := Build("workspace/.rungrid.yaml", []string{"../api", "line\nbreak"})
	if strings.Contains(document.Instructions, "line\nbreak") {
		t.Fatal("project path escaped the JSON data block")
	}
	for _, expected := range []string{"\"manifest_path\": \"workspace/.rungrid.yaml\"", "\"../api\"", "\"line\\nbreak\""} {
		if !strings.Contains(document.Instructions, expected) {
			t.Errorf("instructions missing encoded input %q", expected)
		}
	}
	if document.CanonicalCommand != "instructions" || document.Alias != "agent-start" {
		t.Fatalf("unexpected command identity: %#v", document)
	}
}

func TestBuildCopiesProjectPaths(t *testing.T) {
	t.Parallel()
	paths := []string{"../api"}
	document := Build(".rungrid.yaml", paths)
	paths[0] = "../changed"
	if document.Inputs.ProjectPaths[0] != "../api" {
		t.Fatal("document retained caller-owned project path storage")
	}
}
