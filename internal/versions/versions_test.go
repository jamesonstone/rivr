package versions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jamesonstone/rungrid/internal/subprocess"
)

func TestListeningPortsParsesAndSortsLsofOutput(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "lsof")
	script := "#!/bin/sh\nprintf 'p42\\nn127.0.0.1:9000\\nn*:8080\\nn[::1]:9000\\n'\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	if ports := listeningPorts(context.Background(), 42); !reflect.DeepEqual(ports, []int{8080, 9000}) {
		t.Fatalf("unexpected ports %#v", ports)
	}
}

func TestGitVersionReportsBranchCommitCleanDirtyAndWorktree(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	runGitTest(t, directory, "init", "-b", "main")
	runGitTest(t, directory, "config", "user.name", "Example User")
	runGitTest(t, directory, "config", "user.email", "example@example.invalid")
	filename := filepath.Join(directory, "README.md")
	if err := os.WriteFile(filename, []byte("example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, directory, "add", "README.md")
	runGitTest(t, directory, "commit", "-m", "initial")
	branch, commit, stateValue, worktree := gitVersion(context.Background(), directory)
	if branch != "main" || commit == "" || stateValue != "clean" || worktree != filepath.Base(directory) {
		t.Fatalf("unexpected clean Git version branch=%q commit=%q state=%q worktree=%q", branch, commit, stateValue, worktree)
	}
	if err := os.WriteFile(filename, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, stateValue, _ = gitVersion(context.Background(), directory)
	if stateValue != "dirty" {
		t.Fatalf("expected dirty state, got %q", stateValue)
	}
}

func runGitTest(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := subprocess.Combined(command); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
