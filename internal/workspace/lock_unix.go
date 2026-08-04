//go:build darwin || linux

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/state"
)

type Lock struct {
	file *os.File
}

func Acquire(ctx context.Context, layout state.Layout) (*Lock, error) {
	if err := layout.Ensure(); err != nil {
		return nil, err
	}
	filename := filepath.Join(layout.ProjectDir, "locks", "lifecycle.lock")
	fd, err := syscall.Open(filename, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, errs.Wrap(errs.ExitConflict, "RG1510", "open project lifecycle lock", err)
	}
	file := os.NewFile(uintptr(fd), filename)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errs.New(errs.ExitConflict, "RG1511", "create project lifecycle lock handle")
	}
	closeOnError := func(err error) (*Lock, error) {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return closeOnError(errs.New(errs.ExitConflict, "RG1512", "project lifecycle lock is not a regular file"))
	}
	if err := file.Chmod(0o600); err != nil {
		return closeOnError(errs.Wrap(errs.ExitConflict, "RG1513", "secure project lifecycle lock", err))
	}
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			heldInfo, heldErr := file.Stat()
			pathInfo, pathErr := os.Stat(filename)
			if heldErr != nil || pathErr != nil || !os.SameFile(heldInfo, pathInfo) {
				_ = syscall.Flock(fd, syscall.LOCK_UN)
				_ = file.Close()
				if ctx.Err() != nil {
					return nil, errs.Wrap(errs.ExitInterrupted, "RG1518", "reacquire replaced project lifecycle lock", ctx.Err())
				}
				return Acquire(ctx, layout)
			}
			return &Lock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return closeOnError(errs.Wrap(errs.ExitConflict, "RG1514", "acquire project lifecycle lock", err))
		}
		select {
		case <-ctx.Done():
			return closeOnError(errs.Wrap(errs.ExitInterrupted, "RG1515", "wait for project lifecycle lock", ctx.Err()))
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	var result error
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		result = errs.Wrap(errs.ExitPartial, "RG1516", "release project lifecycle lock", err)
	}
	if err := l.file.Close(); err != nil && result == nil {
		result = errs.Wrap(errs.ExitPartial, "RG1517", "close project lifecycle lock", err)
	}
	return result
}
