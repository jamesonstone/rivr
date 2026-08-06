//go:build darwin || linux

package local_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/subprocess"
)

func TestRepositoryMaintenanceEndToEnd(t *testing.T) {
	if os.Getenv("RUNGRID_E2E") != "1" {
		t.Skip("set RUNGRID_E2E=1 to run the real Process Compose lifecycle")
	}
	if _, err := exec.LookPath("process-compose"); err != nil {
		t.Skip("Process Compose is unavailable")
	}
	directory := t.TempDir()
	binary, _ := buildRungrid(t, directory)
	workspace := filepath.Join(directory, "workspace")
	control := filepath.Join(workspace, "control")
	api := filepath.Join(workspace, "api")
	remote := filepath.Join(directory, "remote.git")
	writer := filepath.Join(directory, "writer")
	for _, path := range []string{workspace, control} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	maintenanceGit(t, directory, "init", "--bare", "--initial-branch=main", remote)
	maintenanceGit(t, directory, "init", "--initial-branch=main", api)
	maintenanceGit(t, api, "config", "user.name", "Example User")
	maintenanceGit(t, api, "config", "user.email", "example@example.com")
	if err := os.WriteFile(filepath.Join(api, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	maintenanceGit(t, api, "add", "README.md")
	maintenanceGit(t, api, "commit", "-m", "initial")
	maintenanceGit(t, api, "remote", "add", "origin", remote)
	maintenanceGit(t, api, "push", "-u", "origin", "main")
	maintenanceGit(t, directory, "clone", remote, writer)
	maintenanceGit(t, writer, "config", "user.name", "Example User")
	maintenanceGit(t, writer, "config", "user.email", "example@example.com")

	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Maintenance, slug: maintenance, id: maintenance-k7m4q2}
workspace: {root: ..}
repositories:
  api: {path: api, remote: origin}
terminal: {mode: headless, open: false}
services:
  - name: api-workspace
    source: native
    repository: api
    activation: workspace
    run: {argv: [sh, -c, "while true; do sleep 1; done"]}
  - name: api-tab
    source: native
    repository: api
    activation: tab
    run: {argv: [sh, -c, "while true; do sleep 1; done"]}
`
	config := filepath.Join(control, ".rungrid.yaml")
	if err := os.WriteFile(config, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDirectory := filepath.Join(directory, "state")
	base := []string{"--config", config, "--state-dir", stateDirectory}
	runMaintenanceCLI(t, binary, base, "up", "--no-open")
	t.Cleanup(func() { _ = exec.Command(binary, append(base, "down")...).Run() })
	waitForE2EState(t, binary, base, "api-workspace", "Running")
	session := exec.Command(binary, append(base, "session", "api-tab")...)
	var sessionOutput bytes.Buffer
	session.Stdout = &sessionOutput
	session.Stderr = &sessionOutput
	if err := session.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Process.Signal(os.Interrupt)
		_ = session.Wait()
	})
	waitForE2EState(t, binary, base, "api-tab", "Running")
	if err := os.WriteFile(filepath.Join(writer, "README.md"), []byte("updated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	maintenanceGit(t, writer, "add", "README.md")
	maintenanceGit(t, writer, "commit", "-m", "updated")
	maintenanceGit(t, writer, "push", "origin", "main")
	remoteOID := maintenanceGit(t, writer, "rev-parse", "HEAD")
	localOID := maintenanceGit(t, api, "rev-parse", "main")
	runMaintenanceCLI(t, binary, base, "sync", "--dry-run")
	if afterDryRun := maintenanceGit(t, api, "rev-parse", "main"); afterDryRun != localOID {
		t.Fatalf("dry-run changed local default from %s to %s", localOID, afterDryRun)
	}
	waitForE2EState(t, binary, base, "api-workspace", "Running")
	waitForE2EState(t, binary, base, "api-tab", "Running")

	output := runMaintenanceCLI(t, binary, base, "--json", "sync")
	var envelope struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(output, &envelope) != nil || envelope.Kind != "RepositorySyncReport" {
		t.Fatalf("sync output omitted typed report: %s", output)
	}
	if localOID := maintenanceGit(t, api, "rev-parse", "main"); localOID != remoteOID {
		t.Fatalf("local default = %s, want %s", localOID, remoteOID)
	}
	waitForE2EState(t, binary, base, "api-workspace", "Running")
	waitForE2EState(t, binary, base, "api-tab", "Running")
	duplicate := exec.Command(binary, append(base, "session", "api-tab")...)
	if duplicateOutput, err := subprocess.Combined(duplicate); err == nil || !strings.Contains(string(duplicateOutput), "already has an owning session") {
		t.Fatalf("maintenance lost tab ownership: err=%v output=%s", err, duplicateOutput)
	}
	if err := session.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := session.Wait(); err == nil {
		t.Fatal("interrupted session unexpectedly succeeded")
	}
	waitForE2EState(t, binary, base, "api-tab", "Completed")
	runMaintenanceCLI(t, binary, base, "down")
}

func maintenanceGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := subprocess.Combined(command)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func runMaintenanceCLI(t *testing.T, binary string, base []string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command(binary, append(base, arguments...)...)
	output, err := subprocess.Combined(command)
	if err != nil {
		t.Fatalf("rungrid %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return output
}
