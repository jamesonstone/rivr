package subprocess

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"
)

func TestRunSeparatesOutputAndPreservesExitCode(t *testing.T) {
	command := exec.Command("sh", "-c", "printf stdout; printf stderr >&2; exit 7")
	result, err := Run(command)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 7 {
		t.Fatalf("error = %v", err)
	}
	if string(result.Stdout) != "stdout" || string(result.Stderr) != "stderr" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStopsOutputAtHardLimit(t *testing.T) {
	command := exec.Command("sh", "-c", "while :; do printf 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef; done")
	result, err := Run(command)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("error = %v", err)
	}
	if len(result.Stdout) != DefaultLimit || !result.StdoutTruncated {
		t.Fatalf("stdout bytes = %d, truncated = %t", len(result.Stdout), result.StdoutTruncated)
	}
}

func TestCombinedSharesOneLimit(t *testing.T) {
	command := exec.Command("sh", "-c", "while :; do printf aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; printf bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb >&2; done")
	output, err := Combined(command)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("error = %v", err)
	}
	if len(output) != DefaultLimit || !bytes.Contains(output, []byte("a")) || !bytes.Contains(output, []byte("b")) {
		t.Fatalf("combined bytes = %d", len(output))
	}
}
