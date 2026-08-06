package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
)

const ownershipAPI = "rungrid/output/v1"

type Layout struct {
	StateRoot  string
	ProjectID  string
	ProjectDir string
}

type ProjectMarker struct {
	APIVersion string `json:"api_version"`
	ProjectID  string `json:"project_id"`
}

func NewLayout(projectID, override string) (Layout, error) {
	if projectID == "" || strings.ContainsAny(projectID, `/\`) || projectID == "." || projectID == ".." {
		return Layout{}, errs.New(errs.ExitUsage, "RG201", "invalid project id for state path")
	}
	root := override
	if root == "" {
		var err error
		root, err = defaultStateRoot()
		if err != nil {
			return Layout{}, err
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, errs.Wrap(errs.ExitUsage, "RG202", "resolve state directory", err)
	}
	absRoot = filepath.Clean(absRoot)
	return Layout{
		StateRoot:  absRoot,
		ProjectID:  projectID,
		ProjectDir: filepath.Join(absRoot, "rungrid", "projects", projectID),
	}, nil
}

func defaultStateRoot() (string, error) {
	if configured := os.Getenv("XDG_STATE_HOME"); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", errs.New(errs.ExitUsage, "RG203", "XDG_STATE_HOME must be absolute")
		}
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errs.Wrap(errs.ExitFailure, "RG204", "resolve user home for state", err)
	}
	if runtime.GOOS == "windows" {
		return "", errs.New(errs.ExitDependency, "RG205", "Windows state paths are not supported in v1")
	}
	return filepath.Join(home, ".local", "state"), nil
}

func (l Layout) Ensure() error {
	if err := ensureStateRoot(l.StateRoot); err != nil {
		return err
	}
	current := l.StateRoot
	for _, component := range []string{"rungrid", "projects", l.ProjectID} {
		var err error
		current, err = ensurePrivateChild(current, component)
		if err != nil {
			return err
		}
	}
	for _, component := range []string{"generations", "lifecycle-logs", "sessions", "tabs", "locks", "maintenance"} {
		if _, err := ensurePrivateChild(l.ProjectDir, component); err != nil {
			return err
		}
	}
	markerPath := filepath.Join(l.ProjectDir, "project.json")
	if content, err := os.ReadFile(markerPath); err == nil {
		var marker ProjectMarker
		if json.Unmarshal(content, &marker) != nil || marker.APIVersion != ownershipAPI || marker.ProjectID != l.ProjectID {
			return errs.New(errs.ExitConflict, "RG241", "project state ownership marker does not match")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitConflict, "RG242", "read project state ownership marker", err)
	}
	markerBytes, err := json.MarshalIndent(ProjectMarker{APIVersion: ownershipAPI, ProjectID: l.ProjectID}, "", "  ")
	if err != nil {
		return errs.Wrap(errs.ExitFailure, "RG243", "encode project state ownership marker", err)
	}
	return WriteFileAtomic(l.ProjectDir, "project.json", append(markerBytes, '\n'), 0o600)
}

func ensureStateRoot(directory string) error {
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errs.New(errs.ExitConflict, "RG251", fmt.Sprintf("state root is not a real directory: %s", directory))
		}
		return nil
	} else if !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitConflict, "RG252", "inspect state root", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG253", "create state root", err)
	}
	return nil
}

func ensurePrivateChild(parent, component string) (string, error) {
	if component == "" || component == "." || component == ".." || strings.ContainsAny(component, `/\`) {
		return "", errs.New(errs.ExitConflict, "RG254", "invalid private state path component")
	}
	directory := filepath.Join(parent, component)
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errs.New(errs.ExitConflict, "RG255", fmt.Sprintf("state path is not a real directory: %s", directory))
		}
		if info.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(directory, 0o700); err != nil {
				return "", errs.Wrap(errs.ExitConflict, "RG256", "secure private state directory", err)
			}
		}
		return directory, nil
	} else if !os.IsNotExist(err) {
		return "", errs.Wrap(errs.ExitConflict, "RG257", "inspect private state directory", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return "", errs.Wrap(errs.ExitFailure, "RG258", "create private state directory", err)
	}
	return directory, nil
}

func (l Layout) VerifyMarker() error {
	content, err := os.ReadFile(filepath.Join(l.ProjectDir, "project.json"))
	if err != nil {
		return errs.Wrap(errs.ExitConflict, "RG244", "read project state ownership marker", err)
	}
	var marker ProjectMarker
	if json.Unmarshal(content, &marker) != nil || marker.APIVersion != ownershipAPI || marker.ProjectID != l.ProjectID {
		return errs.New(errs.ExitConflict, "RG245", "project state ownership marker does not match")
	}
	return nil
}

func ensurePrivateDirectory(directory string) error {
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errs.New(errs.ExitConflict, "RG206", fmt.Sprintf("state path is not a real directory: %s", directory))
		}
		if info.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(directory, 0o700); err != nil {
				return errs.Wrap(errs.ExitConflict, "RG207", "secure state directory", err)
			}
		}
		return nil
	} else if !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitConflict, "RG208", "inspect state directory", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errs.Wrap(errs.ExitFailure, "RG209", "create state directory", err)
	}
	return os.Chmod(directory, 0o700)
}
