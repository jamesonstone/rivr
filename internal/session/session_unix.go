//go:build darwin || linux

package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/procidentity"
	"github.com/jamesonstone/rungrid/internal/serviceexec"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
)

type Registration struct {
	APIVersion      string `json:"api_version"`
	ProjectID       string `json:"project_id"`
	GenerationID    string `json:"generation_id"`
	Service         string `json:"service"`
	PID             int    `json:"pid"`
	ProcessIdentity string `json:"process_identity"`
	TabID           string `json:"tab_id,omitempty"`
	StartedAt       string `json:"started_at"`
}

type Lock struct {
	file         *os.File
	registration string
	identity     Registration
}

func Acquire(layout state.Layout, generationID, service, tabID string) (*Lock, error) {
	if err := layout.Ensure(); err != nil {
		return nil, err
	}
	shutdownMarker := filepath.Join(layout.ProjectDir, "locks", "down-"+generationID+".json")
	if _, err := os.Lstat(shutdownMarker); err == nil {
		return nil, errs.New(errs.ExitConflict, "RG815", "workspace shutdown is in progress")
	} else if !os.IsNotExist(err) {
		return nil, errs.Wrap(errs.ExitConflict, "RG816", "inspect workspace shutdown marker", err)
	}
	lockPath := filepath.Join(layout.ProjectDir, "locks", generationID+"-"+service+".lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errs.Wrap(errs.ExitConflict, "RG801", "open service session lock", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, errs.Wrap(errs.ExitConflict, "RG802", "secure service session lock", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errs.New(errs.ExitConflict, "RG803", fmt.Sprintf("service %s already has an owning session", service))
	}
	if _, err := os.Lstat(shutdownMarker); err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, errs.New(errs.ExitConflict, "RG817", "workspace shutdown started while acquiring session ownership")
	}
	registration := filepath.Join(layout.ProjectDir, "sessions", generationID+"-"+service+".json")
	processIdentity, err := procidentity.Current()
	if err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, errs.Wrap(errs.ExitConflict, "RG818", "record service session process identity", err)
	}
	identity := Registration{
		APIVersion:      "rungrid/output/v1",
		ProjectID:       layout.ProjectID,
		GenerationID:    generationID,
		Service:         service,
		PID:             os.Getpid(),
		ProcessIdentity: processIdentity,
		TabID:           tabID,
		StartedAt:       state.RuntimeTimestamp(),
	}
	content, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, errs.Wrap(errs.ExitFailure, "RG804", "encode session registration", err)
	}
	if err := state.WriteFileAtomic(filepath.Dir(registration), filepath.Base(registration), append(content, '\n'), 0o600); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file, registration: registration, identity: identity}, nil
}

func (l *Lock) Release() error {
	var result error
	if content, err := os.ReadFile(l.registration); err == nil {
		var current Registration
		if json.Unmarshal(content, &current) == nil && current == l.identity {
			if err := os.Remove(l.registration); err != nil && !os.IsNotExist(err) {
				result = errs.Wrap(errs.ExitPartial, "RG805", "remove service session registration", err)
			}
		}
	}
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil && result == nil {
		result = errs.Wrap(errs.ExitPartial, "RG806", "unlock service session", err)
	}
	if err := l.file.Close(); err != nil && result == nil {
		result = errs.Wrap(errs.ExitPartial, "RG807", "close service session lock", err)
	}
	return result
}

func Active(layout state.Layout, generationID, service string) (Registration, bool) {
	filename := filepath.Join(layout.ProjectDir, "sessions", generationID+"-"+service+".json")
	content, err := os.ReadFile(filename)
	if err != nil {
		return Registration{}, false
	}
	var registration Registration
	if json.Unmarshal(content, &registration) != nil || registration.ProjectID != layout.ProjectID || registration.GenerationID != generationID || registration.Service != service {
		return Registration{}, false
	}
	if !procidentity.Matches(registration.PID, registration.ProcessIdentity) {
		return Registration{}, false
	}
	return registration, true
}

type Options struct {
	Layout   state.Layout
	Runtime  supervisor.Runtime
	Manifest *manifest.Manifest
	Service  string
	TabID    string
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
}

func Run(ctx context.Context, options Options) (returnErr error) {
	service, exists := manifest.FindService(options.Manifest, options.Service)
	if !exists {
		return errs.New(errs.ExitUsage, "RG808", "unknown service: "+options.Service)
	}
	if service.Activation != "tab" || service.Source == "external" {
		return errs.New(errs.ExitUsage, "RG809", "session requires a tab-owned native or Compose service")
	}
	if err := supervisor.Verify(ctx, options.Layout, options.Runtime); err != nil {
		return err
	}
	for dependency := range service.DependsOn {
		candidate, _ := manifest.FindService(options.Manifest, dependency)
		if candidate != nil && candidate.Source == "external" {
			dependencyContext, cancel := context.WithTimeout(ctx, options.Manifest.Runtime.StartupTimeout.Duration)
			err := serviceexec.WaitExternal(dependencyContext, options.Manifest, options.Runtime.WorkspaceRoot, candidate)
			cancel()
			if err != nil {
				return err
			}
		}
	}
	lock, err := Acquire(options.Layout, options.Runtime.GenerationID, options.Service, options.TabID)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.Release(); returnErr == nil && releaseErr != nil {
			returnErr = releaseErr
		}
	}()

	client := supervisor.Client(options.Layout, options.Runtime)
	if current, getErr := client.Get(ctx, options.Service); getErr == nil && isRunning(current.Status) {
		return errs.New(errs.ExitConflict, "RG810", "tab-owned service is already running without this session")
	}
	if err := client.Start(ctx, options.Service); err != nil {
		return err
	}
	owned := true
	defer func() {
		if owned {
			stopContext, cancel := context.WithTimeout(context.Background(), options.Manifest.Runtime.ShutdownTimeout.Duration)
			_ = client.Stop(stopContext, options.Service)
			cancel()
		}
	}()

	logContext, cancelLogs := context.WithCancel(context.Background())
	defer cancelLogs()
	logCommand := client.LogsCommand(logContext, []string{options.Service}, true, -1, true, options.Stdin, options.Stdout, options.Stderr)
	if err := logCommand.Start(); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG811", "start service log foreground", err)
	}
	logResult := make(chan error, 1)
	go func() { logResult <- logCommand.Wait() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	started := false
	for {
		select {
		case <-ctx.Done():
			stopContext, cancel := context.WithTimeout(context.Background(), options.Manifest.Runtime.ShutdownTimeout.Duration)
			_ = client.Stop(stopContext, options.Service)
			cancel()
			owned = false
			cancelLogs()
			select {
			case <-logResult:
			case <-time.After(2 * time.Second):
				if logCommand.Process != nil {
					_ = logCommand.Process.Signal(os.Interrupt)
				}
			}
			return errs.Wrap(errs.ExitInterrupted, "RG812", "service session interrupted", ctx.Err())
		case err := <-logResult:
			if err != nil && !errors.Is(err, context.Canceled) {
				return errs.Wrap(errs.ExitFailure, "RG813", "service log foreground ended", err)
			}
			return nil
		case <-ticker.C:
			current, getErr := client.Get(ctx, options.Service)
			if getErr != nil {
				continue
			}
			if isRunning(current.Status) {
				started = true
				continue
			}
			if started || isTerminal(current.Status) {
				owned = false
				cancelLogs()
				select {
				case <-logResult:
				case <-time.After(2 * time.Second):
					if logCommand.Process != nil {
						_ = logCommand.Process.Signal(os.Interrupt)
					}
				}
				if current.ExitCode != 0 {
					return errs.New(errs.ExitFailure, "RG814", fmt.Sprintf("service %s exited with code %d", options.Service, current.ExitCode))
				}
				return nil
			}
		}
	}
}

func isRunning(status string) bool {
	normalized := strings.ToLower(status)
	return strings.Contains(normalized, "running") || strings.Contains(normalized, "launch") || strings.Contains(normalized, "pending")
}

func isTerminal(status string) bool {
	normalized := strings.ToLower(status)
	return strings.Contains(normalized, "complete") || strings.Contains(normalized, "stopped") || strings.Contains(normalized, "disabled") || strings.Contains(normalized, "error") || strings.Contains(normalized, "skipped")
}

func ClientFor(options Options) processcompose.Client {
	return supervisor.Client(options.Layout, options.Runtime)
}
