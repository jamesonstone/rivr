package maintenance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

type repositoryFixture struct {
	root    string
	remote  string
	primary string
	writer  string
}

func newRepositoryFixture(t *testing.T) repositoryFixture {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	primary := filepath.Join(root, "primary")
	writer := filepath.Join(root, "writer")
	gitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
	gitTest(t, root, "init", "--initial-branch=main", primary)
	configureGitUser(t, primary)
	writeTestFile(t, filepath.Join(primary, "README.md"), "initial\n")
	gitTest(t, primary, "add", "README.md")
	gitTest(t, primary, "commit", "-m", "initial")
	gitTest(t, primary, "remote", "add", "origin", remote)
	gitTest(t, primary, "push", "-u", "origin", "main")
	gitTest(t, root, "clone", remote, writer)
	configureGitUser(t, writer)
	return repositoryFixture{root: root, remote: remote, primary: primary, writer: writer}
}

func (fixture repositoryFixture) advanceRemote(t *testing.T, value string) string {
	t.Helper()
	writeTestFile(t, filepath.Join(fixture.writer, "README.md"), value+"\n")
	gitTest(t, fixture.writer, "add", "README.md")
	gitTest(t, fixture.writer, "commit", "-m", value)
	gitTest(t, fixture.writer, "push", "origin", "main")
	return gitTest(t, fixture.writer, "rev-parse", "HEAD")
}

func loadedRepository(t *testing.T, root, repositoryPath string) *manifest.Loaded {
	t.Helper()
	relative, err := filepath.Rel(root, repositoryPath)
	if err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Project:    manifest.Project{Name: "Example", Slug: "example", ID: "example-k7m4q2"},
		Repositories: map[string]manifest.Repository{
			"api": {Path: relative, Remote: "origin"},
		},
		Terminal: manifest.Terminal{Mode: "headless"},
		Services: []manifest.Service{},
	}
	value.ApplyDefaults()
	return &manifest.Loaded{Manifest: value, WorkspaceRoot: root, ManifestDir: root}
}

func gitTest(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func configureGitUser(t *testing.T, directory string) {
	t.Helper()
	gitTest(t, directory, "config", "user.name", "Example User")
	gitTest(t, directory, "config", "user.email", "example@example.com")
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type githubRunner struct {
	CommandRunner
	pullRequests []PullRequest
}

func (runner githubRunner) Run(ctx context.Context, directory, executable string, arguments ...string) ([]byte, error) {
	if executable == "gh" {
		return jsonPullRequests(runner.pullRequests)
	}
	if executable == "lsof" {
		return nil, nil
	}
	if executable == "git" && strings.Join(arguments, "\x00") == "remote\x00get-url\x00origin" {
		return []byte("git@github.com:example/repository.git\n"), nil
	}
	return runner.CommandRunner.Run(ctx, directory, executable, arguments...)
}
