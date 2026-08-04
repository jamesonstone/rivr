package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestGenerateDoesNotExecuteLifecycleCommands(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hook := filepath.Join(root, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch lifecycle-ran\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
terminal: {mode: headless}
lifecycle:
  before_up:
    - {name: prepare, run: {argv: [./hook]}}
services: []
`
	filename := filepath.Join(root, ".rungrid.yaml")
	if err := os.WriteFile(filename, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manifest.Load(filename, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(loaded, t.TempDir(), "test", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lifecycle-ran")); !os.IsNotExist(err) {
		t.Fatalf("generate executed a lifecycle command: %v", err)
	}
}
