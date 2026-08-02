package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
)

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
		Artifact: Artifact{Path: filepath.ToSlash(clean), Kind: kind, SHA256: hex.EncodeToString(hash[:]), Mode: uint32(mode.Perm())},
		Content:  append([]byte(nil), content...),
	})
	return nil
}

func (b *Builder) Promote(checkOnly bool) (string, bool, error) {
	if err := b.layout.Ensure(); err != nil {
		return "", false, err
	}
	destination := filepath.Join(b.layout.ProjectDir, "generations", b.generationID)
	ownership := Ownership{APIVersion: ownershipAPI, ProjectID: b.layout.ProjectID, GenerationID: b.generationID, GeneratorVersion: b.version, Artifacts: make([]Artifact, len(b.artifacts))}
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
