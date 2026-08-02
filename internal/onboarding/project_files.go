package onboarding

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
)

func WriteProjectFiles(options Options, content []byte) error {
	destination := options.Destination
	if destination == "" {
		destination = filepath.Join(options.Root, ".rungrid.yaml")
	}
	if _, err := os.Lstat(destination); err == nil && !options.Force {
		return errs.New(errs.ExitConflict, "RG1308", "manifest already exists; use --force after review")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := atomicWrite(destination, content, 0o644); err != nil {
		return err
	}
	gitignore := filepath.Join(options.Root, ".gitignore")
	existing, err := os.ReadFile(gitignore)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)
	for _, line := range []string{".rungrid.local.yaml", ".rungrid.draft.json"} {
		if !containsLine(text, line) {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += line + "\n"
		}
	}
	if err := atomicWrite(gitignore, []byte(text), 0o644); err != nil {
		return err
	}
	if options.DraftPath != "" {
		_ = os.Remove(options.DraftPath)
	}
	return nil
}

func containsLine(content, line string) bool {
	for _, current := range strings.Split(content, "\n") {
		if strings.TrimSpace(current) == line {
			return true
		}
	}
	return false
}

func atomicWrite(filename string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errs.New(errs.ExitConflict, "RG1309", "refusing to replace a symlink: "+filename)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".rungrid-init-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, filename)
}
