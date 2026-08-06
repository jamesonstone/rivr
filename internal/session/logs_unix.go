//go:build darwin || linux

package session

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/processcompose"
)

type logFollower struct {
	command *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	result  error
}

func startLogFollower(client processcompose.Client, service string, stdin io.Reader, stdout, stderr io.Writer) (*logFollower, error) {
	ctx, cancel := context.WithCancel(context.Background())
	command := client.LogsCommand(ctx, []string{service}, true, -1, true, stdin, stdout, stderr)
	if err := command.Start(); err != nil {
		cancel()
		return nil, errs.Wrap(errs.ExitFailure, "RG811", "start service log foreground", err)
	}
	follower := &logFollower{command: command, cancel: cancel, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		follower.mu.Lock()
		follower.result = err
		follower.mu.Unlock()
		close(follower.done)
	}()
	return follower, nil
}

func (f *logFollower) channel() <-chan struct{} {
	if f == nil {
		return nil
	}
	return f.done
}

func (f *logFollower) err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.result
}

func (f *logFollower) stop() error {
	if f == nil {
		return nil
	}
	f.cancel()
	select {
	case <-f.done:
		err := f.err()
		if err != nil && !errors.Is(err, context.Canceled) {
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) {
				return err
			}
		}
		return nil
	case <-time.After(2 * time.Second):
		if f.command.Process != nil {
			_ = f.command.Process.Signal(os.Interrupt)
		}
		return nil
	}
}
