package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

type Ownership struct {
	APIVersion       string     `json:"api_version"`
	ProjectID        string     `json:"project_id"`
	GenerationID     string     `json:"generation_id"`
	GeneratorVersion string     `json:"generator_version"`
	Artifacts        []Artifact `json:"artifacts"`
}

type Artifact struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

type pendingArtifact struct {
	Artifact
	Content []byte
}

type Builder struct {
	layout       Layout
	generationID string
	version      string
	artifacts    []pendingArtifact
}

func NewLayout(projectID, override string) (Layout, error) {
	if projectID == "" || strings.ContainsAny(projectID, `/\\`) || projectID == "." || projectID == ".." {
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
	for _, component := range []string{"generations", "sessions", "tabs", "locks"} {
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
	if component == "" || component == "." || component == ".." || strings.ContainsAny(component, `/\\`) {
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

func NewBuilder(layout Layout, generationID, version string) *Builder {
	return &Builder{layout: layout, generationID: generationID, version: version}
}

func (b *Builder) Add(relative, kind string, content []byte, mode fs.FileMode) error {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errs.New(errs.ExitUsage, "RG210", fmt.Sprintf("unsafe artifact path: %s", relative))
	}
	for _, existing := range b.artifacts {
		if existing.Path == filepath.ToSlash(clean) {
			return errs.New(errs.ExitUsage, "RG211", fmt.Sprintf("duplicate artifact path: %s", relative))
		}
	}
	hash := sha256.Sum256(content)
	b.artifacts = append(b.artifacts, pendingArtifact{
		Artifact: Artifact{
			Path:   filepath.ToSlash(clean),
			Kind:   kind,
			SHA256: hex.EncodeToString(hash[:]),
			Mode:   uint32(mode.Perm()),
		},
		Content: append([]byte(nil), content...),
	})
	return nil
}

func (b *Builder) Promote(checkOnly bool) (string, bool, error) {
	if err := b.layout.Ensure(); err != nil {
		return "", false, err
	}
	destination := filepath.Join(b.layout.ProjectDir, "generations", b.generationID)
	ownership := Ownership{
		APIVersion:       ownershipAPI,
		ProjectID:        b.layout.ProjectID,
		GenerationID:     b.generationID,
		GeneratorVersion: b.version,
		Artifacts:        make([]Artifact, len(b.artifacts)),
	}
	for i, artifact := range b.artifacts {
		ownership.Artifacts[i] = artifact.Artifact
	}
	ownershipBytes, err := json.MarshalIndent(ownership, "", "  ")
	if err != nil {
		return "", false, errs.Wrap(errs.ExitFailure, "RG212", "encode ownership metadata", err)
	}
	ownershipBytes = append(ownershipBytes, '\n')

	if _, err := os.Lstat(destination); err == nil {
		matches, verifyErr := verifyGeneration(destination, ownership, b.artifacts)
		if verifyErr != nil {
			return "", false, verifyErr
		}
		if !matches {
			return "", false, errs.New(errs.ExitConflict, "RG213", fmt.Sprintf("generated state was modified: %s", destination))
		}
		if checkOnly {
			return destination, false, nil
		}
		if err := WriteFileAtomic(b.layout.ProjectDir, "current", []byte(b.generationID+"\n"), 0o600); err != nil {
			return "", false, err
		}
		return destination, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, errs.Wrap(errs.ExitConflict, "RG214", "inspect generation destination", err)
	}
	if checkOnly {
		return destination, true, errs.New(errs.ExitConflict, "RG215", "generated state is missing or stale")
	}

	parent := filepath.Dir(destination)
	temporary, err := os.MkdirTemp(parent, ".generation-")
	if err != nil {
		return "", false, errs.Wrap(errs.ExitFailure, "RG216", "create generation staging directory", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return "", false, errs.Wrap(errs.ExitFailure, "RG217", "secure generation staging directory", err)
	}
	for _, artifact := range b.artifacts {
		filename := filepath.Join(temporary, filepath.FromSlash(artifact.Path))
		if err := writeNewFile(temporary, filename, artifact.Content, fs.FileMode(artifact.Mode)); err != nil {
			return "", false, err
		}
	}
	if err := writeNewFile(temporary, filepath.Join(temporary, "ownership.json"), ownershipBytes, 0o600); err != nil {
		return "", false, err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", false, errs.Wrap(errs.ExitConflict, "RG218", "promote generated state", err)
	}
	cleanup = false
	if err := syncDirectory(parent); err != nil {
		return "", false, err
	}
	if err := WriteFileAtomic(b.layout.ProjectDir, "current", []byte(b.generationID+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return destination, true, nil
}

func verifyGeneration(directory string, expected Ownership, artifacts []pendingArtifact) (bool, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errs.New(errs.ExitConflict, "RG219", "generation path is not an owned directory")
	}
	content, err := os.ReadFile(filepath.Join(directory, "ownership.json"))
	if err != nil {
		return false, errs.Wrap(errs.ExitConflict, "RG220", "read generation ownership", err)
	}
	var actual Ownership
	if err := json.Unmarshal(content, &actual); err != nil {
		return false, errs.Wrap(errs.ExitConflict, "RG221", "decode generation ownership", err)
	}
	if actual.APIVersion != expected.APIVersion || actual.ProjectID != expected.ProjectID || actual.GenerationID != expected.GenerationID || actual.GeneratorVersion != expected.GeneratorVersion || len(actual.Artifacts) != len(expected.Artifacts) {
		return false, nil
	}
	actualByPath := make(map[string]Artifact, len(actual.Artifacts))
	for _, item := range actual.Artifacts {
		actualByPath[item.Path] = item
	}
	for _, expectedArtifact := range artifacts {
		recorded, exists := actualByPath[expectedArtifact.Path]
		if !exists || recorded != expectedArtifact.Artifact {
			return false, nil
		}
		filename := filepath.Join(directory, filepath.FromSlash(expectedArtifact.Path))
		info, err := os.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || uint32(info.Mode().Perm()) != expectedArtifact.Mode {
			return false, nil
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			return false, nil
		}
		hash := sha256.Sum256(content)
		if hex.EncodeToString(hash[:]) != expectedArtifact.SHA256 {
			return false, nil
		}
	}
	return true, nil
}

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
	rel, err := filepath.Rel(base, filename)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
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

func CurrentGeneration(layout Layout) (string, error) {
	content, err := os.ReadFile(filepath.Join(layout.ProjectDir, "current"))
	if err != nil {
		return "", errs.Wrap(errs.ExitConflict, "RG239", "read current generation", err)
	}
	value := strings.TrimSpace(string(content))
	if value == "" || strings.ContainsAny(value, `/\\`) {
		return "", errs.New(errs.ExitConflict, "RG240", "invalid current generation identifier")
	}
	return value, nil
}

func RecordedRuntimeGeneration(layout Layout) (string, bool, error) {
	filename := filepath.Join(layout.ProjectDir, "runtime.json")
	info, statErr := os.Lstat(filename)
	if os.IsNotExist(statErr) {
		return "", false, nil
	}
	if statErr != nil {
		return "", false, errs.Wrap(errs.ExitConflict, "RG246", "inspect runtime generation guard", statErr)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", false, errs.New(errs.ExitConflict, "RG247", "runtime generation guard is not a private regular file")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", false, errs.Wrap(errs.ExitConflict, "RG248", "read runtime generation guard", err)
	}
	var record struct {
		APIVersion   string `json:"api_version"`
		ProjectID    string `json:"project_id"`
		GenerationID string `json:"generation_id"`
	}
	if json.Unmarshal(content, &record) != nil || record.APIVersion != ownershipAPI || record.ProjectID != layout.ProjectID || record.GenerationID == "" {
		return "", false, errs.New(errs.ExitConflict, "RG249", "runtime generation guard is invalid")
	}
	return record.GenerationID, true, nil
}

func Hash(parts ...[]byte) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write(part)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func RuntimeTimestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
