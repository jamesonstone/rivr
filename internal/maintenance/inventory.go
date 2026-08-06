package maintenance

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func Discover(ctx context.Context, loaded *manifest.Loaded, selected []string, runner Runner) ([]Repository, []Failure) {
	names, failures := selectedRepositoryNames(&loaded.Manifest, selected)
	byCommonDir := make(map[string]int)
	repositories := make([]Repository, 0, len(names))
	for _, name := range names {
		repository, err := discoverRepository(ctx, loaded, name, runner)
		if err != nil {
			failures = append(failures, Failure{Repository: name, Operation: "discover", Error: err.Error()})
			continue
		}
		if index, exists := byCommonDir[repository.CommonDir]; exists {
			repositories[index].Aliases = append(repositories[index].Aliases, name)
			repositories[index].DeclaredPaths = append(repositories[index].DeclaredPaths, repository.TopLevel)
			continue
		}
		byCommonDir[repository.CommonDir] = len(repositories)
		repositories = append(repositories, repository)
	}
	return repositories, failures
}

func selectedRepositoryNames(m *manifest.Manifest, selected []string) ([]string, []Failure) {
	known := map[string]bool{manifest.WorkspaceRepository: true}
	for name := range m.Repositories {
		known[name] = true
	}
	if len(selected) == 0 {
		implicitWorkspace := len(m.Repositories) == 0
		for index := range m.Services {
			implicitWorkspace = implicitWorkspace || m.Services[index].Repository == manifest.WorkspaceRepository
		}
		names := make([]string, 0, len(known))
		for name := range known {
			if name == manifest.WorkspaceRepository && !implicitWorkspace {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	seen := make(map[string]bool)
	var names []string
	var failures []Failure
	for _, name := range selected {
		if !known[name] {
			failures = append(failures, Failure{Repository: name, Operation: "select", Error: "unknown repository"})
			continue
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, failures
}

func discoverRepository(ctx context.Context, loaded *manifest.Loaded, name string, runner Runner) (Repository, error) {
	root, err := manifest.RepositoryRoot(&loaded.Manifest, loaded.WorkspaceRoot, name)
	if err != nil {
		return Repository{}, err
	}
	top, err := gitText(ctx, runner, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repository{}, fmt.Errorf("repository path is not a Git worktree")
	}
	top, err = physicalPath(top)
	if err != nil {
		return Repository{}, fmt.Errorf("resolve Git top-level: %w", err)
	}
	root, err = physicalPath(root)
	if err != nil {
		return Repository{}, fmt.Errorf("resolve declared repository: %w", err)
	}
	common, err := gitText(ctx, runner, top, "rev-parse", "--git-common-dir")
	if err != nil {
		return Repository{}, fmt.Errorf("discover Git common directory: %w", err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	common, err = physicalPath(common)
	if err != nil {
		return Repository{}, fmt.Errorf("resolve Git common directory: %w", err)
	}
	entries, err := listWorktrees(ctx, runner, top)
	if err != nil || len(entries) == 0 {
		return Repository{}, fmt.Errorf("discover primary worktree: %w", err)
	}
	configuration := manifest.RepositoryConfiguration(&loaded.Manifest, name)
	remoteURL, err := gitText(ctx, runner, top, "remote", "get-url", configuration.Remote)
	if err != nil {
		return Repository{}, fmt.Errorf("remote %q is unavailable", configuration.Remote)
	}
	return Repository{
		Name: name, Path: root, TopLevel: top, CommonDir: common,
		Primary: entries[0].Path, Remote: configuration.Remote,
		DefaultBranch: configuration.DefaultBranch, RemoteSlug: githubSlug(remoteURL),
		DeclaredPaths: []string{top},
	}, nil
}

func physicalPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(absolute))
}

func githubSlug(remote string) string {
	value := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if strings.HasPrefix(value, "git@github.com:") {
		value = strings.TrimPrefix(value, "git@github.com:")
		if validGitHubSlug(value) {
			return value
		}
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && strings.EqualFold(parsed.Host, "github.com") {
		value = strings.TrimPrefix(parsed.Path, "/")
		if validGitHubSlug(value) {
			return value
		}
	}
	return ""
}

func validGitHubSlug(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, character := range part {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && !strings.ContainsRune("._-", character) {
				return false
			}
		}
	}
	return true
}

func canonicalRoot(repository Repository) string {
	home, err := os.UserHomeDir()
	if err != nil || repository.RemoteSlug == "" {
		return ""
	}
	return filepath.Join(home, "worktrees", filepath.FromSlash(repository.RemoteSlug))
}
