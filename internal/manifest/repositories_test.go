package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredRepositoryContainsServicePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	control := filepath.Join(root, "control")
	api := filepath.Join(root, "api")
	mustMkdir(t, control)
	mustMkdir(t, api)
	mustWrite(t, filepath.Join(api, "compose.yaml"), "services: {api: {image: example/api}}\n")
	mustWrite(t, filepath.Join(control, ".rungrid.yaml"), `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: ..}
repositories:
  api: {path: api}
terminal: {mode: headless}
services:
  - name: api
    repository: api
    source: compose
    activation: workspace
    compose: {file: compose.yaml, service: api}
`)
	loaded, err := Load(filepath.Join(control, ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	service := &loaded.Manifest.Services[0]
	if service.Repository != "api" || service.WorkingDirectory != "." {
		t.Fatalf("repository defaults changed: %#v", service)
	}
	serviceRoot, err := ServiceRepositoryRoot(&loaded.Manifest, loaded.WorkspaceRoot, service)
	if err != nil {
		t.Fatal(err)
	}
	resolvedAPI, _ := filepath.EvalSymlinks(api)
	if serviceRoot != resolvedAPI {
		t.Fatalf("service root = %q, want %q", serviceRoot, resolvedAPI)
	}
	if strings.Contains(string(loaded.MergedYAML), resolvedAPI) {
		t.Fatal("normalized manifest persisted an absolute repository path")
	}
}

func TestRepositoryOverlayReplacesPortablePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	control := filepath.Join(root, "control")
	mustMkdir(t, control)
	mustMkdir(t, filepath.Join(root, "api-default"))
	mustMkdir(t, filepath.Join(root, "api-local"))
	mustWrite(t, filepath.Join(control, ".rungrid.yaml"), repositoryManifest("api-default", "."))
	mustWrite(t, filepath.Join(control, ".rungrid.local.yaml"), "repositories:\n  api: {path: api-local}\n")
	loaded, err := Load(filepath.Join(control, ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.Repositories["api"].Path != "api-local" {
		t.Fatalf("repository overlay was not applied: %#v", loaded.Manifest.Repositories)
	}
}

func TestRepositoryValidationRejectsUnsafeDeclarations(t *testing.T) {
	t.Parallel()
	for name, setup := range map[string]func(*testing.T, string, string) string{
		"absolute": func(t *testing.T, root, _ string) string {
			return repositoryManifest(filepath.Join(root, "api"), ".")
		},
		"duplicate-workspace": func(_ *testing.T, _, _ string) string {
			return repositoryManifest(".", ".")
		},
		"duplicate-declaration": func(_ *testing.T, _, _ string) string {
			return strings.Replace(repositoryManifest("api", "."), "api: {path: api}", "api: {path: api}\n  web: {path: api}", 1)
		},
		"reserved-name": func(_ *testing.T, _, _ string) string {
			return strings.Replace(repositoryManifest("api", "."), "api: {path: api}", "workspace: {path: api}", 1)
		},
		"unknown-service-reference": func(_ *testing.T, _, _ string) string {
			return strings.Replace(repositoryManifest("api", "."), "repository: api", "repository: missing", 1)
		},
		"service-escape": func(_ *testing.T, _, _ string) string {
			return repositoryManifest("api", "../web")
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			control := filepath.Join(root, "control")
			mustMkdir(t, control)
			mustMkdir(t, filepath.Join(root, "api"))
			mustMkdir(t, filepath.Join(root, "web"))
			mustWrite(t, filepath.Join(control, ".rungrid.yaml"), setup(t, root, control))
			if _, err := Load(filepath.Join(control, ".rungrid.yaml"), ""); err == nil {
				t.Fatal("expected repository validation failure")
			}
		})
	}
}

func TestRepositoryValidationRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	control := filepath.Join(root, "control")
	mustMkdir(t, control)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "api")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(control, ".rungrid.yaml"), repositoryManifest("api", "."))
	_, err := Load(filepath.Join(control, ".rungrid.yaml"), "")
	if err == nil || !strings.Contains(err.Error(), "resolves outside the workspace") {
		t.Fatalf("expected repository symlink rejection, got %v", err)
	}
}

func repositoryManifest(repositoryPath, workingDirectory string) string {
	return `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: ..}
repositories:
  api: {path: ` + repositoryPath + `}
terminal: {mode: headless}
services:
  - name: api
    repository: api
    source: native
    activation: workspace
    working_directory: ` + workingDirectory + `
    run: {argv: [true]}
`
}
