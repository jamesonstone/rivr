//go:build darwin || linux

package local_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/subprocess"
)

const testOutputLimit = 32 << 10

type evidenceResult struct {
	Result           string `json:"result"`
	ExitCode         int    `json:"exit_code"`
	FailureKind      string `json:"failure_kind"`
	OutputBytes      int64  `json:"output_bytes"`
	OutputLimitBytes int64  `json:"output_limit_bytes"`
	OutputTruncated  bool   `json:"output_truncated"`
}

func TestEvidenceRunnerPreservesResultWithoutReplayingOutput(t *testing.T) {
	helper, script := evidenceTestTools(t, `#!/bin/sh
printf 'child-output-marker\n'
exit "${FAKE_GO_EXIT:-0}"
`)
	for _, test := range []struct {
		name       string
		childExit  int
		wantResult string
	}{
		{name: "success", wantResult: "PASS"},
		{name: "failure", childExit: 7, wantResult: "FAIL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "evidence")
			command := evidenceCommand(t, helper, script, root)
			command.Env = append(command.Env, "FAKE_GO_EXIT="+strconv.Itoa(test.childExit))
			terminal, err := subprocess.Combined(command)
			assertExitCode(t, err, test.childExit)
			if bytes.Contains(terminal, []byte("child-output-marker")) {
				t.Fatalf("runner replayed full evidence to terminal: %q", terminal)
			}
			if len(terminal) > 1024 {
				t.Fatalf("runner terminal output is unbounded: %d bytes", len(terminal))
			}
			runDirectory := onlyRunDirectory(t, root)
			content, err := os.ReadFile(filepath.Join(runDirectory, "output.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "child-output-marker\n" {
				t.Fatalf("output.txt = %q", content)
			}
			result := readEvidenceResult(t, runDirectory)
			if result.Result != test.wantResult || result.ExitCode != test.childExit {
				t.Fatalf("result = %#v", result)
			}
			if result.OutputBytes != int64(len(content)) || result.OutputTruncated {
				t.Fatalf("output metadata = %#v", result)
			}
		})
	}
}

func TestEvidenceRunnerOutputLimitStopsDescendants(t *testing.T) {
	helper, script := evidenceTestTools(t, `#!/bin/sh
sleep 30 &
printf '%s\n' "$!" >"$DESCENDANT_PID_FILE"
while :; do
  printf '0123456789abcdef0123456789abcdef\n'
done
`)
	root := filepath.Join(t.TempDir(), "evidence")
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	command := evidenceCommand(t, helper, script, root)
	command.Env = append(command.Env, "DESCENDANT_PID_FILE="+pidFile)
	terminal, err := subprocess.Combined(command)
	assertExitCode(t, err, 74)
	if len(terminal) > 1024 {
		t.Fatalf("runner terminal output is unbounded: %d bytes", len(terminal))
	}
	runDirectory := onlyRunDirectory(t, root)
	info, err := os.Stat(filepath.Join(runDirectory, "output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != testOutputLimit {
		t.Fatalf("bounded output size = %d, want %d", info.Size(), testOutputLimit)
	}
	result := readEvidenceResult(t, runDirectory)
	if result.FailureKind != "output_limit" || !result.OutputTruncated || result.OutputLimitBytes != testOutputLimit {
		t.Fatalf("overflow result = %#v", result)
	}
	assertRecordedProcessGone(t, pidFile)
}

func TestEvidenceRunnerSignalWritesResultAndStopsDescendants(t *testing.T) {
	helper, script := evidenceTestTools(t, `#!/bin/sh
sleep 30 &
printf '%s\n' "$!" >"$DESCENDANT_PID_FILE"
printf 'started\n'
wait
`)
	root := filepath.Join(t.TempDir(), "evidence")
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	command := evidenceCommand(t, helper, script, root)
	command.Env = append(command.Env, "DESCENDANT_PID_FILE="+pidFile)
	var terminal bytes.Buffer
	command.Stdout = &terminal
	command.Stderr = &terminal
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, pidFile)
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	assertExitCode(t, command.Wait(), 130)
	if len(terminal.Bytes()) > 1024 {
		t.Fatalf("runner terminal output is unbounded: %d bytes", terminal.Len())
	}
	result := readEvidenceResult(t, onlyRunDirectory(t, root))
	if result.FailureKind != "signal" || result.ExitCode != 130 {
		t.Fatalf("signal result = %#v", result)
	}
	assertRecordedProcessGone(t, pidFile)
}

func TestEvidenceRunnerConcurrentRunsRemainUnique(t *testing.T) {
	helper, script := evidenceTestTools(t, "#!/bin/sh\nprintf 'ok\\n'\n")
	root := filepath.Join(t.TempDir(), "evidence")
	commands := []*exec.Cmd{
		evidenceCommand(t, helper, script, root),
		evidenceCommand(t, helper, script, root),
	}
	outputs := make([]bytes.Buffer, len(commands))
	for index, command := range commands {
		command.Stdout = &outputs[index]
		command.Stderr = &outputs[index]
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("concurrent runner: %v\n%s", err, outputs[index].Bytes())
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() == entries[1].Name() {
		t.Fatalf("run directories = %#v", entries)
	}
}

func evidenceTestTools(t *testing.T, fakeGo string) (string, string) {
	t.Helper()
	repositoryRoot := evidenceRepositoryRoot(t)
	helper := filepath.Join(t.TempDir(), "rungrid-evidence")
	build := exec.Command("go", "build", "-o", helper, "./tests/evidencecapture")
	build.Dir = repositoryRoot
	if output, err := subprocess.Combined(build); err != nil {
		t.Fatalf("build evidence helper: %v\n%s", err, output)
	}
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "go")
	if err := os.WriteFile(script, []byte(fakeGo), 0o700); err != nil {
		t.Fatal(err)
	}
	return helper, script
}

func evidenceCommand(t *testing.T, helper, fakeGo, root string) *exec.Cmd {
	t.Helper()
	repositoryRoot := evidenceRepositoryRoot(t)
	command := exec.Command("sh", filepath.Join(repositoryRoot, "tests", "end-to-end", "local", "run.sh"))
	command.Dir = repositoryRoot
	command.Env = append(os.Environ(),
		"PATH="+filepath.Dir(fakeGo)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RUNGRID_EVIDENCE_HELPER="+helper,
		"RUNGRID_EVIDENCE_ROOT="+root,
		fmt.Sprintf("RUNGRID_EVIDENCE_LIMIT_BYTES=%d", testOutputLimit),
	)
	return command
}

func evidenceRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func onlyRunDirectory(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("run entries = %#v", entries)
	}
	return filepath.Join(root, entries[0].Name())
}

func readEvidenceResult(t *testing.T, runDirectory string) evidenceResult {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(runDirectory, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result evidenceResult
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, content)
	}
	return result
}

func assertExitCode(t *testing.T, err error, expected int) {
	t.Helper()
	if expected == 0 && err == nil {
		return
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != expected {
		t.Fatalf("exit error = %v, want %d", err, expected)
	}
}

func waitForFile(t *testing.T, filename string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filename); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", filename)
}

func assertRecordedProcessGone(t *testing.T, filename string) {
	t.Helper()
	waitForFile(t, filename)
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant PID %d remains", pid)
}
