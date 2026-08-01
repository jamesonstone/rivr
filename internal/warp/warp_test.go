package warp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
)

func TestTemplatesAreOverviewVersionsThenManifestOrderedTabs(t *testing.T) {
	t.Parallel()
	loaded, err := manifest.Load(filepath.Join("..", "..", "testdata", "example", ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	templates := Templates(&loaded.Manifest, "generation")
	if len(templates) != 4 {
		t.Fatalf("got %d templates", len(templates))
	}
	for index, suffix := range []string{"_00_overview", "_01_versions", "_02_api", "_03_web"} {
		if !strings.HasSuffix(templates[index].TabName, suffix) {
			t.Errorf("template %d name %q does not end in %q", index, templates[index].TabName, suffix)
		}
		if strings.Contains(string(templates[index].Content), loaded.Root) {
			t.Errorf("template %d persists an absolute workspace path", index)
		}
	}
}

func TestURIsOpenFullWorkspaceInNewWindowAndAbsentServiceInPlace(t *testing.T) {
	t.Parallel()
	record := InstallRecord{Artifacts: []InstalledFile{
		{Tab: "workspace_00_overview"},
		{Tab: "workspace_01_versions"},
		{Tab: "workspace_02_api", Service: "api"},
	}}
	full, err := URIs(record, "")
	if err != nil {
		t.Fatal(err)
	}
	wantFull := []string{"warp://tab_config/workspace_00_overview?new_window=true", "warp://tab_config/workspace_01_versions", "warp://tab_config/workspace_02_api"}
	if len(full) != len(wantFull) {
		t.Fatalf("unexpected URI count %#v", full)
	}
	for i := range full {
		if full[i] != wantFull[i] {
			t.Errorf("URI %d=%q, want %q", i, full[i], wantFull[i])
		}
	}
	service, err := URIs(record, "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(service) != 1 || service[0] != "warp://tab_config/workspace_02_api" {
		t.Fatalf("unexpected absent-service URI %#v", service)
	}
}

func TestInstallAndUninstallOwnOnlyRecordedWarpFiles(t *testing.T) {
	root := t.TempDir()
	warpDirectory := filepath.Join(root, "warp")
	t.Setenv("WARP_TAB_CONFIG_DIR", warpDirectory)
	loaded, err := manifest.Load(filepath.Join("..", "..", "testdata", "example", ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := state.NewLayout(loaded.Manifest.Project.ID, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	record, err := Install(layout, &loaded.Manifest, "generation", "/example path/rungrid")
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Artifacts) != 4 {
		t.Fatalf("got %d installed files", len(record.Artifacts))
	}
	content, err := os.ReadFile(record.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `commands = ["exec '/example path/rungrid' --state-dir`) {
		t.Fatalf("installed command is not TOML-escaped as expected:\n%s", content)
	}
	unrelated := filepath.Join(warpDirectory, "unrelated.toml")
	if err := os.WriteFile(unrelated, []byte("name = \"unrelated\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(layout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("uninstall changed unrelated file: %v", err)
	}
	for _, artifact := range record.Artifacts {
		if _, err := os.Stat(artifact.Path); !os.IsNotExist(err) {
			t.Errorf("owned tab remains: %s", artifact.Path)
		}
	}
}

func TestModifiedWarpFileFailsClosed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WARP_TAB_CONFIG_DIR", filepath.Join(root, "warp"))
	loaded, err := manifest.Load(filepath.Join("..", "..", "testdata", "example", ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := state.NewLayout(loaded.Manifest.Project.ID, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := Install(layout, &loaded.Manifest, "generation", "rungrid")
	if err != nil {
		t.Fatal(err)
	}
	modified := record.Artifacts[0].Path
	content, err := os.ReadFile(modified)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modified, append(content, []byte("# user edit\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(layout, &loaded.Manifest, "generation", "rungrid"); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("expected install ownership conflict, got %v", err)
	}
	if err := Uninstall(layout); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("expected uninstall ownership conflict, got %v", err)
	}
	if _, err := os.Stat(modified); err != nil {
		t.Fatalf("modified user file was removed: %v", err)
	}
}
