//go:build darwin || linux

package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jamesonstone/rungrid/internal/environment"
	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
)

const maxLifecycleOutput = 4 << 20

func Execute(
	ctx context.Context,
	layout state.Layout,
	generationID string,
	workspaceRoot string,
	phase string,
	index int,
	configuration manifest.LifecycleCommand,
) (CommandOutcome, error) {
	started := time.Now().UTC()
	outcome := CommandOutcome{Phase: phase, Name: configuration.Name, StartedAt: started.Format(time.RFC3339Nano)}
	runContext, cancel := context.WithTimeout(ctx, configuration.Timeout.Duration)
	defer cancel()

	directory := filepath.Join(workspaceRoot, configuration.WorkingDirectory)
	environmentValues, resolved, err := environment.ResolveEnvironment(runContext, configuration.Environment, directory, workspaceRoot)
	if err != nil {
		return finishOutcome(outcome, "failed", 1, "environment resolution failed (details redacted)"), err
	}
	path, err := environment.LookPath(configuration.Run.Argv[0], directory, resolved)
	if err != nil {
		detail := redact(err.Error(), resolved)
		return finishOutcome(outcome, "failed", 127, detail), errs.Wrap(errs.ExitDependency, "RG1520", "resolve lifecycle executable", err)
	}

	command := exec.Command(path, configuration.Run.Argv[1:]...)
	command.Dir = directory
	command.Env = environmentValues
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var capture limitedCapture
	command.Stdout = &capture
	command.Stderr = &capture
	if err := command.Start(); err != nil {
		detail := redact(err.Error(), resolved)
		return finishOutcome(outcome, "failed", 126, detail), errs.Wrap(errs.ExitDependency, "RG1521", "start lifecycle command", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	var commandErr error
	select {
	case commandErr = <-waited:
	case <-runContext.Done():
		commandErr = stopProcessGroup(command, waited)
	}

	status, exitCode := commandStatus(commandErr, ctx, runContext)
	detail := ""
	if commandErr != nil {
		detail = redact(commandErr.Error(), resolved)
	}
	if capture.truncated {
		if detail != "" {
			detail += "; "
		}
		detail += "output truncated"
	}
	outcome = finishOutcome(outcome, status, exitCode, detail)
	logContent := []byte(redact(capture.String(), resolved))
	logPath := filepath.Join(
		"lifecycle-logs",
		generationID,
		fmt.Sprintf("%s-%02d-%s.log", phase, index, configuration.Name),
	)
	if err := state.WriteFileAtomic(layout.ProjectDir, logPath, logContent, 0o600); err != nil {
		outcome.Status = "failed"
		outcome.Detail = "write private lifecycle log failed"
		return outcome, err
	}
	outcome.Log = filepath.ToSlash(logPath)
	if commandErr == nil {
		return outcome, nil
	}
	code := errs.ExitFailure
	if status == "interrupted" {
		code = errs.ExitInterrupted
	}
	return outcome, errs.Wrap(code, "RG1522", "lifecycle command "+configuration.Name+" "+status, commandErr)
}

func stopProcessGroup(command *exec.Cmd, waited <-chan error) error {
	if command.Process == nil {
		return context.Canceled
	}
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	select {
	case err := <-waited:
		return err
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		return <-waited
	}
}

func commandStatus(commandErr error, parent, run context.Context) (string, int) {
	if commandErr == nil {
		return "succeeded", 0
	}
	if parent.Err() != nil {
		return "interrupted", 130
	}
	if errors.Is(run.Err(), context.DeadlineExceeded) {
		return "timed-out", 124
	}
	var exitError *exec.ExitError
	if errors.As(commandErr, &exitError) {
		return "failed", exitError.ExitCode()
	}
	return "failed", 1
}

func finishOutcome(outcome CommandOutcome, status string, exitCode int, detail string) CommandOutcome {
	outcome.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	outcome.Status = status
	outcome.ExitCode = exitCode
	outcome.Detail = detail
	return outcome
}

func redact(value string, environmentValues map[string]string) string {
	secrets := make([]string, 0, len(environmentValues))
	for key, candidate := range environmentValues {
		if manifest.SecretLikeKey(key) && len(candidate) >= 4 {
			secrets = append(secrets, candidate)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

type limitedCapture struct {
	bytes.Buffer
	truncated bool
}

func (b *limitedCapture) Write(content []byte) (int, error) {
	original := len(content)
	remaining := maxLifecycleOutput - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(content) > remaining {
		_, _ = b.Buffer.Write(content[:remaining])
		b.truncated = true
		return original, nil
	}
	_, _ = b.Buffer.Write(content)
	return original, nil
}
