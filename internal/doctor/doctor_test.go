package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestDoctorChecksLifecycleExecutableWithoutRunningIt(t *testing.T) {
	root := t.TempDir()
	processCompose := filepath.Join(root, "process-compose")
	if err := os.WriteFile(processCompose, []byte("#!/bin/sh\nprintf 'Version: v1.120.0\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	githubCLI := filepath.Join(root, "gh")
	githubArguments := filepath.Join(root, "gh-arguments")
	if err := os.WriteFile(githubCLI, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_GH_ARGUMENTS\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GH_ARGUMENTS", githubArguments)
	hook := filepath.Join(root, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch lifecycle-ran\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
runtime: {process_compose: {executable: process-compose}}
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
	report := Run(context.Background(), loaded, t.TempDir(), false)
	if !report.OK || !hasCheck(report, "lifecycle:before_up:prepare", "ok") {
		t.Fatalf("doctor did not validate lifecycle executable: %#v", report)
	}
	if _, err := os.Stat(filepath.Join(root, "lifecycle-ran")); !os.IsNotExist(err) {
		t.Fatalf("doctor executed lifecycle hook: %v", err)
	}
	arguments, err := os.ReadFile(githubArguments)
	if err != nil || string(arguments) != "auth\nstatus\n--hostname\ngithub.com\n" {
		t.Fatalf("GitHub authentication arguments = %q, err=%v", arguments, err)
	}
}

func TestRequiredExecutablesIncludesStructuredWrappersAndTabTrigger(t *testing.T) {
	t.Parallel()
	configuration := manifest.Manifest{Services: []manifest.Service{{
		Name: "api", Source: "native", Activation: "tab",
		Run:      &manifest.Run{Argv: []string{"env", "-u", "PC_LOG_LEVEL", "direnv", "exec", ".", "sh", "-c", "exec npm run dev"}},
		Terminal: manifest.ServiceTerminal{TriggerArgv: []string{"make", "dev"}},
	}}}
	actual := requiredExecutables(&configuration)
	for _, expected := range []string{"env", "direnv", "sh", "make"} {
		if !containsExecutable(actual, expected) {
			t.Errorf("required executable %q missing from %#v", expected, actual)
		}
	}
}

func TestDoctorReportsDeclaredRepository(t *testing.T) {
	workspace := t.TempDir()
	control := filepath.Join(workspace, "control")
	api := filepath.Join(workspace, "api")
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(api, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", api, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("initialize repository: %v\n%s", err, output)
	}
	processCompose := filepath.Join(workspace, "process-compose")
	if err := os.WriteFile(processCompose, []byte("#!/bin/sh\nprintf 'Version: v1.120.0\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workspace+string(os.PathListSeparator)+os.Getenv("PATH"))
	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
workspace: {root: ..}
repositories: {api: {path: api}}
runtime: {process_compose: {executable: process-compose}}
terminal: {mode: headless}
services: []
`
	filename := filepath.Join(control, ".rungrid.yaml")
	if err := os.WriteFile(filename, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manifest.Load(filename, "")
	if err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), loaded, t.TempDir(), false)
	if !report.OK || !hasCheck(report, "repository:api", "ok") || !hasCheck(report, "repository-git:api", "ok") {
		t.Fatalf("doctor omitted repository checks: %#v", report)
	}
}

func containsExecutable(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func hasCheck(report Report, name, status string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
