package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestImplicitWorkspaceSelectionRequiresWorkspaceOwnership(t *testing.T) {
	configuration := manifest.Manifest{
		Repositories: map[string]manifest.Repository{"api": {Path: "api", Remote: "origin"}},
	}
	configuration.ApplyDefaults()
	names, failures := selectedRepositoryNames(&configuration, nil)
	if len(failures) != 0 || len(names) != 1 || names[0] != "api" {
		t.Fatalf("declared-only selection = %v, failures = %v", names, failures)
	}
	configuration.Services = []manifest.Service{{Name: "control", Repository: manifest.WorkspaceRepository}}
	names, failures = selectedRepositoryNames(&configuration, nil)
	if len(failures) != 0 || len(names) != 2 || names[0] != "api" || names[1] != manifest.WorkspaceRepository {
		t.Fatalf("workspace-owned selection = %v, failures = %v", names, failures)
	}
}

func TestDiscoverDeduplicatesWorktreesFromOneRepository(t *testing.T) {
	fixture := newRepositoryFixture(t)
	feature := filepath.Join(fixture.root, "feature")
	gitTest(t, fixture.primary, "worktree", "add", "-b", "feature", feature, "main")
	loaded := loadedRepository(t, fixture.root, fixture.primary)
	relative, err := filepath.Rel(fixture.root, feature)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Manifest.Repositories["web"] = manifest.Repository{Path: relative, Remote: "origin"}
	repositories, failures := Discover(context.Background(), loaded, nil, githubRunner{})
	if len(failures) != 0 || len(repositories) != 1 {
		t.Fatalf("repositories = %#v, failures = %#v", repositories, failures)
	}
	if len(repositories[0].Aliases) != 1 || repositories[0].Aliases[0] != "web" || len(repositories[0].DeclaredPaths) != 2 {
		t.Fatalf("deduplicated repository = %#v", repositories[0])
	}
}

func TestDiscoverAcceptsDeclaredDirectoryInsideGitWorktree(t *testing.T) {
	fixture := newRepositoryFixture(t)
	subdirectory := filepath.Join(fixture.primary, "services", "api")
	if err := os.MkdirAll(subdirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	loaded := loadedRepository(t, fixture.root, subdirectory)
	repositories, failures := Discover(context.Background(), loaded, []string{"api"}, githubRunner{})
	if len(failures) != 0 || len(repositories) != 1 {
		t.Fatalf("repositories = %#v, failures = %#v", repositories, failures)
	}
	top, _ := physicalPath(fixture.primary)
	if repositories[0].TopLevel != top || len(repositories[0].DeclaredPaths) != 1 || repositories[0].DeclaredPaths[0] != top {
		t.Fatalf("repository top-level = %#v", repositories[0])
	}
}

func TestGitHubSlugRejectsTraversalAndUnexpectedPaths(t *testing.T) {
	for _, remote := range []string{
		"git@github.com:../repository.git",
		"https://github.com/example/../repository.git",
		"https://github.com/example/repository/extra.git",
	} {
		if slug := githubSlug(remote); slug != "" {
			t.Errorf("githubSlug(%q) = %q", remote, slug)
		}
	}
	if slug := githubSlug("git@github.com:example/repository.git"); slug != "example/repository" {
		t.Fatalf("valid slug = %q", slug)
	}
}
