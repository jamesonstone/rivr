package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
)

const WorkspaceRepository = "workspace"

func DeclaredRepositoryNames(m *Manifest) []string {
	names := make([]string, 0, len(m.Repositories))
	for name := range m.Repositories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func RepositoryRoot(m *Manifest, workspaceRoot, name string) (string, error) {
	resolvedWorkspace, err := resolveExisting(workspaceRoot)
	if err != nil {
		return "", errs.Wrap(errs.ExitConflict, "RG132", "resolve runtime workspace repository", err)
	}
	if name == "" || name == WorkspaceRepository {
		return resolvedWorkspace, nil
	}
	repository, exists := m.Repositories[name]
	if !exists {
		return "", errs.New(errs.ExitUsage, "RG133", "unknown service repository: "+name)
	}
	resolved, err := resolveRepositoryPath(resolvedWorkspace, repository.Path)
	if err != nil {
		return "", errs.Wrap(errs.ExitConflict, "RG134", "resolve service repository "+name, err)
	}
	return resolved, nil
}

func ServiceRepositoryRoot(m *Manifest, workspaceRoot string, service *Service) (string, error) {
	return RepositoryRoot(m, workspaceRoot, service.Repository)
}

func ServiceWorkingDirectory(m *Manifest, workspaceRoot string, service *Service) (string, error) {
	repositoryRoot, err := ServiceRepositoryRoot(m, workspaceRoot, service)
	if err != nil {
		return "", err
	}
	resolved, err := resolveExisting(filepath.Join(repositoryRoot, service.WorkingDirectory))
	if err != nil {
		return "", errs.Wrap(errs.ExitConflict, "RG135", "resolve service working directory", err)
	}
	if !within(repositoryRoot, resolved) {
		return "", errs.New(errs.ExitConflict, "RG136", "service working directory resolves outside its repository")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errs.New(errs.ExitConflict, "RG137", "service working directory is not a directory")
	}
	return resolved, nil
}

func validateRepositories(root string, repositories map[string]Repository, add func(string, string)) map[string]string {
	resolved := map[string]string{WorkspaceRepository: root}
	owners := map[string]string{root: WorkspaceRepository}
	for _, name := range sortedRepositoryNames(repositories) {
		field := "repositories." + name
		repository := repositories[name]
		valid := true
		if !serviceNamePattern.MatchString(name) {
			add(field, "name must match [a-z][a-z0-9-]*")
			valid = false
		}
		if name == WorkspaceRepository {
			add(field, "workspace is reserved for the implicit workspace root")
			valid = false
		}
		if strings.TrimSpace(repository.Path) == "" {
			add(field+".path", "is required")
			valid = false
		} else if filepath.IsAbs(repository.Path) {
			add(field+".path", "must be workspace-relative")
			valid = false
		}
		if !valid {
			continue
		}
		repositoryRoot, err := resolveRepositoryPath(root, repository.Path)
		if err != nil {
			add(field+".path", err.Error())
			continue
		}
		if owner, duplicate := owners[repositoryRoot]; duplicate {
			add(field+".path", fmt.Sprintf("resolves to the same directory as repository %q", owner))
			continue
		}
		owners[repositoryRoot] = name
		resolved[name] = repositoryRoot
	}
	return resolved
}

func resolveRepositoryPath(root, declared string) (string, error) {
	resolved, err := resolveExisting(filepath.Join(root, filepath.Clean(declared)))
	if err != nil {
		return "", fmt.Errorf("must name an existing directory")
	}
	if !within(root, resolved) {
		return "", fmt.Errorf("resolves outside the workspace")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("must name an existing directory")
	}
	return resolved, nil
}

func sortedRepositoryNames(repositories map[string]Repository) []string {
	names := make([]string, 0, len(repositories))
	for name := range repositories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
