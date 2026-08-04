package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadWorkspaceRootAllowsSiblingRepositoriesAndImports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manifestDirectory := filepath.Join(root, "control")
	mustMkdir(t, manifestDirectory)
	mustMkdir(t, filepath.Join(root, "api"))
	mustWrite(t, filepath.Join(root, "services.yaml"), `services:
  - name: api
    source: native
    activation: workspace
    working_directory: api
    run: {argv: [go, run, .]}
`)
	mustWrite(t, filepath.Join(manifestDirectory, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: ..}
imports: [../services.yaml]
terminal: {mode: headless}
services: []
`)
	loaded, err := Load(filepath.Join(manifestDirectory, ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedManifestDirectory, err := filepath.EvalSymlinks(manifestDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WorkspaceRoot != resolvedRoot {
		t.Fatalf("workspace root = %q, want %q", loaded.WorkspaceRoot, resolvedRoot)
	}
	if loaded.ManifestDir != resolvedManifestDirectory {
		t.Fatalf("manifest directory = %q, want %q", loaded.ManifestDir, resolvedManifestDirectory)
	}
	if loaded.Manifest.Workspace.Root != ".." {
		t.Fatalf("portable root changed to %q", loaded.Manifest.Workspace.Root)
	}
	if len(loaded.Manifest.Services) != 1 || loaded.Manifest.Services[0].WorkingDirectory != "api" {
		t.Fatalf("sibling service was not loaded: %#v", loaded.Manifest.Services)
	}
	if strings.Contains(string(loaded.MergedYAML), resolvedRoot) {
		t.Fatal("normalized manifest persisted an absolute workspace root")
	}
}

func TestLoadRejectsInvalidWorkspaceRoots(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	manifestDirectory := filepath.Join(parent, "control")
	other := filepath.Join(parent, "other")
	mustMkdir(t, manifestDirectory)
	mustMkdir(t, other)
	base := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: %s}
terminal: {mode: headless}
services: []
`
	for name, declared := range map[string]string{
		"absolute":     filepath.ToSlash(other),
		"not-ancestor": "../other",
	} {
		t.Run(name, func(t *testing.T) {
			mustWrite(t, filepath.Join(manifestDirectory, ".rungrid.yaml"), strings.Replace(base, "%s", declared, 1))
			_, err := Load(filepath.Join(manifestDirectory, ".rungrid.yaml"), "")
			if err == nil {
				t.Fatal("expected workspace root rejection")
			}
		})
	}
}

func TestLoadRejectsSymlinkEscapingWorkspaceRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	manifestDirectory := filepath.Join(root, "control")
	mustMkdir(t, manifestDirectory)
	if err := os.Symlink(outside, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(manifestDirectory, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: ..}
terminal: {mode: headless}
services:
  - name: api
    source: native
    activation: workspace
    working_directory: escaped
    run: {argv: [true]}
`)
	_, err := Load(filepath.Join(manifestDirectory, ".rungrid.yaml"), "")
	if err == nil || !strings.Contains(err.Error(), "resolves outside the workspace") {
		t.Fatalf("expected symlink boundary rejection, got %v", err)
	}
}

func TestImportedManifestCannotRedefineWorkspaceRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	control := filepath.Join(root, "control")
	mustMkdir(t, control)
	mustWrite(t, filepath.Join(root, "fragment.yaml"), "workspace: {root: .}\nservices: []\n")
	mustWrite(t, filepath.Join(control, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: ..}
imports: [../fragment.yaml]
terminal: {mode: headless}
services: []
`)
	_, err := Load(filepath.Join(control, ".rungrid.yaml"), "")
	if err == nil || !strings.Contains(err.Error(), "may only be declared in the source manifest") {
		t.Fatalf("expected imported root rejection, got %v", err)
	}
}

func TestLifecycleOverlayReplacesPhaseAndAppliesDefaults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
terminal: {mode: headless}
lifecycle:
  before_up:
    - {name: first, run: {argv: [one]}}
    - {name: second, run: {argv: [two]}}
services: []
`
	overlay := `api_version: rungrid/v1
kind: Workspace
lifecycle:
  before_up:
    - {name: replacement, run: {argv: [three]}}
`
	mustWrite(t, filepath.Join(root, ".rungrid.yaml"), base)
	mustWrite(t, filepath.Join(root, ".rungrid.local.yaml"), overlay)
	loaded, err := Load(filepath.Join(root, ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	commands := loaded.Manifest.Lifecycle.BeforeUp
	if len(commands) != 1 || commands[0].Name != "replacement" {
		t.Fatalf("lifecycle phase was not replaced: %#v", commands)
	}
	if commands[0].WorkingDirectory != "." || commands[0].Timeout != loaded.Manifest.Runtime.StartupTimeout {
		t.Fatalf("lifecycle defaults were not applied: %#v", commands[0])
	}
	if !reflect.DeepEqual(commands[0].Run.Argv, []string{"three"}) {
		t.Fatalf("unexpected argv: %#v", commands[0].Run.Argv)
	}
}

func TestLifecycleRejectsDuplicateNamesAndShellStrings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
terminal: {mode: headless}
lifecycle:
  before_up:
    - {name: prepare, run: {argv: [true]}}
    - {name: prepare, run: {command: "true"}}
services: []
`)
	_, err := Load(filepath.Join(root, ".rungrid.yaml"), "")
	if err == nil || !strings.Contains(err.Error(), "field command not found") {
		t.Fatalf("expected structured-command rejection, got %v", err)
	}
}

func TestLifecycleRejectsDuplicateNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
terminal: {mode: headless}
lifecycle:
  after_down:
    - {name: cleanup, run: {argv: [true]}}
    - {name: cleanup, run: {argv: [true]}}
services: []
`)
	_, err := Load(filepath.Join(root, ".rungrid.yaml"), "")
	if err == nil || !strings.Contains(err.Error(), "duplicates lifecycle.after_down[0]") {
		t.Fatalf("expected duplicate lifecycle name rejection, got %v", err)
	}
}

func TestLifecycleRejectsEmptyArgvAndInvalidTimeout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
terminal: {mode: headless}
lifecycle:
  before_up:
    - name: prepare
      timeout: -1s
      run: {argv: []}
services: []
`)
	_, err := Load(filepath.Join(root, ".rungrid.yaml"), "")
	if err == nil || !strings.Contains(err.Error(), "run.argv: must be a non-empty argument vector") ||
		!strings.Contains(err.Error(), "timeout: must be positive") {
		t.Fatalf("expected argv and timeout validation errors, got %v", err)
	}
}
