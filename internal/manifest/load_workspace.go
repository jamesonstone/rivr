package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jamesonstone/rungrid/internal/errs"
	"gopkg.in/yaml.v3"
)

func resolveDeclaredWorkspaceRoot(configPath string) (string, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", errs.Wrap(errs.ExitUsage, "RG123", "read manifest workspace", err)
	}
	return ResolveWorkspaceRootContent(filepath.Dir(configPath), content)
}

func ResolveWorkspaceRootContent(manifestDirectory string, content []byte) (string, error) {
	var header struct {
		Workspace Workspace `yaml:"workspace"`
	}
	if err := yaml.Unmarshal(content, &header); err != nil {
		return "", errs.Wrap(errs.ExitUsage, "RG124", "decode manifest workspace", err)
	}
	return ResolveWorkspaceRoot(manifestDirectory, header.Workspace.Root)
}

func ResolveWorkspaceRoot(manifestDirectory, declared string) (string, error) {
	if declared == "" {
		declared = "."
	}
	if filepath.IsAbs(declared) {
		return "", errs.New(errs.ExitUsage, "RG125", "workspace.root must be relative to the manifest directory")
	}
	manifestDir, err := resolveExisting(manifestDirectory)
	if err != nil {
		return "", errs.Wrap(errs.ExitUsage, "RG130", "resolve manifest directory", err)
	}
	resolved, err := resolveExisting(filepath.Join(manifestDir, declared))
	if err != nil {
		return "", errs.Wrap(errs.ExitUsage, "RG126", "resolve workspace.root", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errs.New(errs.ExitUsage, "RG127", "workspace.root must name an existing directory")
	}
	if !within(resolved, manifestDir) {
		return "", errs.New(
			errs.ExitUsage,
			"RG128",
			fmt.Sprintf("manifest directory must remain inside workspace.root: %s", declared),
		)
	}
	return resolved, nil
}

func definesWorkspaceRoot(document map[string]any) bool {
	workspace, ok := document["workspace"].(map[string]any)
	if !ok {
		return false
	}
	_, exists := workspace["root"]
	return exists
}
