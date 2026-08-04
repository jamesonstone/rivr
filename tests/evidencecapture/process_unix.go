//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func runCommand(options runnerOptions, output *boundedFile) commandOutcome {
	command := exec.Command(options.command[0], options.command[1:]...)
	command.Dir = options.repositoryRoot
	command.Env = append(os.Environ(), "RUNGRID_E2E=1")
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return commandOutcome{exitCode: 126, failureKind: "runner"}
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case err := <-waited:
		if output.wasTruncated() {
			return commandOutcome{exitCode: 74, failureKind: "output_limit"}
		}
		return commandResult(err)
	case <-output.overflow:
		_ = terminateProcessGroup(command.Process.Pid, waited)
		return commandOutcome{exitCode: 74, failureKind: "output_limit"}
	case received := <-signals:
		_ = terminateProcessGroup(command.Process.Pid, waited)
		value := received.(syscall.Signal)
		return commandOutcome{exitCode: 128 + int(value), failureKind: "signal", signal: signalName(value)}
	}
}

func terminateProcessGroup(pid int, waited <-chan error) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case err := <-waited:
		return err
	case <-time.After(500 * time.Millisecond):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return <-waited
	}
}

func commandResult(err error) commandOutcome {
	if err == nil {
		return commandOutcome{}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			signalValue := status.Signal()
			return commandOutcome{exitCode: 128 + int(signalValue), failureKind: "signal", signal: signalName(signalValue)}
		}
		return commandOutcome{exitCode: exitError.ExitCode(), failureKind: "command"}
	}
	return commandOutcome{exitCode: 1, failureKind: "runner"}
}

func signalName(value syscall.Signal) string {
	if name := value.String(); name != "signal "+strconv.Itoa(int(value)) {
		return name
	}
	return strconv.Itoa(int(value))
}
