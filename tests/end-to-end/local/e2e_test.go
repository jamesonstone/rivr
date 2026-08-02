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
	"time"
)

func TestHeadlessLifecycleEndToEnd(t *testing.T) {
	if os.Getenv("RUNGRID_E2E") != "1" {
		t.Skip("set RUNGRID_E2E=1 to run the real Process Compose lifecycle")
	}
	if _, err := exec.LookPath("process-compose"); err != nil {
		t.Skip("Process Compose is unavailable")
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "rungrid")
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repositoryRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Rungrid: %v\n%s", err, output)
	}
	stateDirectory := filepath.Join(directory, "state")
	workspace := filepath.Join(directory, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "testdata", "headless", ".rungrid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(workspace, ".rungrid.yaml")
	fixture = bytes.Replace(fixture, []byte("mode: headless"), []byte("mode: warp"), 1)
	if err := os.WriteFile(config, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	baseArguments := []string{"--config", config, "--state-dir", stateDirectory}
	run := func(arguments ...string) []byte {
		t.Helper()
		command := exec.Command(binary, append(baseArguments, arguments...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("rungrid %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		return output
	}
	run("up", "--headless", "--no-open")
	t.Cleanup(func() { _ = exec.Command(binary, append(baseArguments, "down")...).Run() })
	runtimePath := filepath.Join(stateDirectory, "rungrid", "projects", "headless-example-r5n2w7", "runtime.json")
	runtimeRecord, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeIdentity struct {
		GenerationID string `json:"generation_id"`
	}
	if err := json.Unmarshal(runtimeRecord, &runtimeIdentity); err != nil {
		t.Fatal(err)
	}
	terminalDirectory := filepath.Join(stateDirectory, "rungrid", "projects", "headless-example-r5n2w7", "generations", runtimeIdentity.GenerationID, "terminal")
	if _, err := os.Stat(terminalDirectory); !os.IsNotExist(err) {
		t.Fatalf("--headless generated graphical terminal files: %v", err)
	}
	var tampered map[string]any
	if err := json.Unmarshal(runtimeRecord, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["pid"] = os.Getpid()
	tamperedContent, _ := json.MarshalIndent(tampered, "", "  ")
	if err := os.WriteFile(runtimePath, append(tamperedContent, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, append(baseArguments, "status")...).CombinedOutput(); err == nil || !strings.Contains(string(output), "runtime PID") {
		t.Fatalf("tampered runtime PID was not rejected: err=%v output=%s", err, output)
	}
	if err := os.WriteFile(runtimePath, runtimeRecord, 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(workspace, ".rungrid.local.yaml")
	if err := os.WriteFile(overlay, []byte("api_version: rungrid/v1\nkind: Workspace\nruntime:\n  startup_timeout: 11s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changedArguments := []string{"--config", config, "--local", overlay, "--state-dir", stateDirectory, "generate"}
	if output, err := exec.Command(binary, changedArguments...).CombinedOutput(); err == nil || !strings.Contains(string(output), "different runtime generation is active") {
		t.Fatalf("active-generation guard did not fail closed: err=%v output=%s", err, output)
	}
	if err := os.Remove(overlay); err != nil {
		t.Fatal(err)
	}

	session := exec.Command(binary, append(baseArguments, "session", "worker")...)
	var sessionOutput bytes.Buffer
	session.Stdout = &sessionOutput
	session.Stderr = &sessionOutput
	if err := session.Start(); err != nil {
		t.Fatal(err)
	}
	waitForE2EState(t, binary, baseArguments, "worker", "Running")
	duplicate := exec.Command(binary, append(baseArguments, "session", "worker")...)
	if output, err := duplicate.CombinedOutput(); err == nil || !strings.Contains(string(output), "already has an owning session") {
		t.Fatalf("duplicate session was not rejected: err=%v output=%s", err, output)
	}
	if err := session.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := session.Wait(); err == nil {
		t.Fatal("interrupted session unexpectedly returned success")
	}
	waitForE2EState(t, binary, baseArguments, "worker", "Completed")

	restarted := exec.Command(binary, append(baseArguments, "session", "worker")...)
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	waitForE2EState(t, binary, baseArguments, "worker", "Running")
	if err := restarted.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	_ = restarted.Wait()
	run("down")
	if _, err := os.Stat(filepath.Join(stateDirectory, "rungrid", "projects", "headless-example-r5n2w7", "runtime.json")); !os.IsNotExist(err) {
		t.Fatalf("runtime record remains after down: %v", err)
	}
}

func waitForE2EState(t *testing.T, binary string, baseArguments []string, service, expected string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		command := exec.Command(binary, append(baseArguments, "--json", "status", service)...)
		output, err := command.Output()
		if err == nil {
			var envelope struct {
				Data struct {
					Services []struct {
						Status string `json:"status"`
					} `json:"services"`
				} `json:"data"`
			}
			if json.Unmarshal(output, &envelope) == nil && len(envelope.Data.Services) == 1 && envelope.Data.Services[0].Status == expected {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("service %s did not reach %s", service, expected)
}
