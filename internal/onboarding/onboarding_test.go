package onboarding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDiscoverComposeAndNativeCommandEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "compose.yaml"), "services:\n  database:\n    image: example/database\n    profiles: [development]\n")
	mustWrite(t, filepath.Join(root, "Makefile"), "dev:\n\t@echo running\n")
	candidates, discoveryHash, err := Discover(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if discoveryHash == "" || len(candidates) != 2 {
		t.Fatalf("unexpected discovery hash=%q candidates=%#v", discoveryHash, candidates)
	}
	if candidates[0].Source != "compose" || candidates[0].ComposeService != "database" {
		t.Fatalf("unexpected Compose discovery %#v", candidates[0])
	}
	if len(candidates[0].Profiles) != 1 || candidates[0].Profiles[0] != "development" {
		t.Fatalf("Compose profiles were not discovered: %#v", candidates[0].Profiles)
	}
	if candidates[1].Source != "native" || strings.Join(candidates[1].Argv, " ") != "make dev" || candidates[1].Confidence != "high" {
		t.Fatalf("unexpected native inference %#v", candidates[1])
	}
}

func TestModelTransitionsBacktrackingResizeAndConfirmation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	candidates := []Candidate{{Name: "api", Source: "native", Directory: ".", Argv: []string{"make", "dev"}, Confidence: "high", Evidence: "Makefile dev target"}}
	m := newModel(Options{Root: root}, candidates, "hash", "example-k7m4q2")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)
	if m.width != 100 || m.height != 30 {
		t.Fatalf("resize was not recorded: %dx%d", m.width, m.height)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != 1 {
		t.Fatalf("project screen did not advance: %d", m.screen)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != 2 {
		t.Fatalf("path screen did not advance: %d", m.screen)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(model)
	if m.terminal != "headless" {
		t.Fatalf("terminal selection did not change: %s", m.terminal)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != 3 {
		t.Fatalf("terminal screen did not advance: %d", m.screen)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	if m.screen != 2 {
		t.Fatalf("backtracking failed: %d", m.screen)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != 4 {
		t.Fatalf("discovery screen did not advance: %d", m.screen)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(model)
	if m.environment != "dotenv" {
		t.Fatalf("environment selection did not change: %s", m.environment)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != 6 {
		t.Fatalf("dependency screen did not advance: %d", m.screen)
	}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if !m.done || command == nil {
		t.Fatal("final review did not confirm and quit")
	}
}

func TestDraftResumeRequiresMatchingDiscovery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	draftPath := filepath.Join(root, ".rungrid.draft.json")
	draft := Draft{APIVersion: "rungrid/output/v1", FlowVersion: 3, DiscoveryHash: "old", Name: "Draft", Terminal: "headless", Environment: "none", LinkDependencies: true, Selected: []bool{true}, Screen: 3}
	content, _ := json.Marshal(draft)
	mustWrite(t, draftPath, string(content))
	candidates := []Candidate{{Name: "api", Source: "native", Confidence: "high"}}
	modelValue := newModel(Options{Root: root, DraftPath: draftPath}, candidates, "new", "example-k7m4q2")
	if modelValue.resumed {
		t.Fatal("stale discovery draft resumed")
	}
	draft.DiscoveryHash = "new"
	content, _ = json.Marshal(draft)
	mustWrite(t, draftPath, string(content))
	modelValue = newModel(Options{Root: root, DraftPath: draftPath}, candidates, "new", "example-k7m4q2")
	if !modelValue.resumed || modelValue.input.Value() != "Draft" || modelValue.screen != 3 {
		t.Fatalf("matching draft did not resume: %#v", modelValue)
	}
}

func TestBuildManifestAppliesProfilesEnvironmentDependenciesAndTriggers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	candidates := []Candidate{
		{Name: "database", Source: "compose", Directory: ".", ComposeFile: "compose.yaml", ComposeService: "database", Profiles: []string{"development"}, Confidence: "exact"},
		{Name: "api", Source: "native", Directory: "api", Argv: []string{"make", "dev"}, Confidence: "high"},
	}
	m := newModel(Options{Root: root}, candidates, "hash", "example-k7m4q2")
	m.input.SetValue("Example")
	m.environment = "dotenv"
	m.linkDependencies = true
	built := m.buildManifest()
	if built.Services[0].Compose == nil || len(built.Services[0].Compose.Profiles) != 1 {
		t.Fatalf("Compose profile was lost: %#v", built.Services[0].Compose)
	}
	api := &built.Services[1]
	if len(api.Environment.Providers) != 1 || api.Environment.Providers[0].Type != "dotenv" || !api.Environment.Providers[0].Optional {
		t.Fatalf("environment choice was lost: %#v", api.Environment.Providers)
	}
	if api.DependsOn["database"] != "running" {
		t.Fatalf("dependency choice was lost: %#v", api.DependsOn)
	}
	if strings.Join(api.Terminal.TriggerArgv, " ") != "make dev" {
		t.Fatalf("trigger inference was lost: %#v", api.Terminal.TriggerArgv)
	}
}

func TestNonInteractiveInitWritesPortableManifestAndIgnoresLocalFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	result, err := NonInteractive(Options{Root: root, Destination: filepath.Join(root, ".rungrid.yaml")}, nil, "Example Workspace", "headless")
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Project.ID == "" || strings.Contains(result.Manifest.Project.ID, root) {
		t.Fatalf("invalid project identity %q", result.Manifest.Project.ID)
	}
	manifestContent, err := os.ReadFile(filepath.Join(root, ".rungrid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestContent), root) {
		t.Fatal("manifest persisted an absolute workspace path")
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{".rungrid.local.yaml", ".rungrid.draft.json"} {
		if !strings.Contains(string(ignore), required) {
			t.Errorf("gitignore is missing %s", required)
		}
	}
}

func TestNonInteractiveInitDiscoversFromParentWorkspaceRoot(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	control := filepath.Join(workspace, "control")
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(workspace, "compose.yaml"), "services:\n  database:\n    image: example/database\n")
	destination := filepath.Join(control, ".rungrid.yaml")
	result, err := NonInteractive(Options{
		Root: control, WorkspaceRoot: "..", Destination: destination, FromCompose: "compose.yaml",
	}, nil, "Example Workspace", "headless")
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Workspace.Root != ".." || len(result.Manifest.Services) != 1 {
		t.Fatalf("parent workspace discovery failed: %#v", result.Manifest)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), workspace) {
		t.Fatal("onboarding persisted an absolute workspace path")
	}
}

func mustWrite(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
