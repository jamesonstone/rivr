package state

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
)

func WriteFileAtomic(base, relative string, content []byte, mode fs.FileMode) error {
	clean := filepath.Clean(relative)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errs.New(errs.ExitConflict, "RG222", "atomic write escapes its base directory")
	}
	if err := ensurePrivateDirectory(base); err != nil {
		return err
	}
	destination := filepath.Join(base, clean)
	if err := ensureRelativeDirectories(base, filepath.Dir(destination)); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errs.New(errs.ExitConflict, "RG223", "refusing to replace a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitConflict, "RG224", "inspect atomic write target", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".write-")
	if err != nil {
		return errs.Wrap(errs.ExitFailure, "RG225", "create atomic write temporary file", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		_ = temporary.Close()
		return errs.Wrap(errs.ExitFailure, "RG226", "permission atomic write temporary file", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return errs.Wrap(errs.ExitFailure, "RG227", "write atomic file", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errs.Wrap(errs.ExitFailure, "RG228", "sync atomic file", err)
	}
	if err := temporary.Close(); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG229", "close atomic file", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG230", "replace atomic file", err)
	}
	return syncDirectory(filepath.Dir(destination))
}

func ensureRelativeDirectories(base, target string) error {
	relative, err := filepath.Rel(base, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errs.New(errs.ExitConflict, "RG259", "atomic write directory escapes its base")
	}
	current := base
	if relative == "." {
		return nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current, err = ensurePrivateChild(current, component)
		if err != nil {
			return err
		}
	}
	return nil
}

func writeNewFile(base, filename string, content []byte, mode fs.FileMode) error {
	relative, err := filepath.Rel(base, filename)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errs.New(errs.ExitConflict, "RG231", "artifact path escapes generation")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG232", "create artifact directory", err)
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return errs.Wrap(errs.ExitConflict, "RG233", "create generated artifact", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return errs.Wrap(errs.ExitFailure, "RG234", "write generated artifact", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errs.Wrap(errs.ExitFailure, "RG235", "sync generated artifact", err)
	}
	if err := file.Close(); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG236", "close generated artifact", err)
	}
	return nil
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return errs.Wrap(errs.ExitFailure, "RG237", "open directory for sync", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errs.Wrap(errs.ExitFailure, "RG238", "sync directory", err)
	}
	if err := file.Close(); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG250", "close synced directory", err)
	}
	return nil
}
