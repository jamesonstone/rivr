package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type worktreeProcessRunner struct {
	githubRunner
	path string
}

type worktreeRemovalFailRunner struct{ CommandRunner }

func (runner worktreeRemovalFailRunner) Run(ctx context.Context, directory, executable string, arguments ...string) ([]byte, error) {
	if executable == "git" && len(arguments) >= 2 && arguments[0] == "worktree" && arguments[1] == "remove" {
		return nil, errors.New("removal refused")
	}
	return runner.CommandRunner.Run(ctx, directory, executable, arguments...)
}

func (runner worktreeProcessRunner) Run(ctx context.Context, directory, executable string, arguments ...string) ([]byte, error) {
	if executable == "lsof" {
		return []byte("p123\nfcwd\nn" + runner.path + "\n"), nil
	}
	return runner.githubRunner.Run(ctx, directory, executable, arguments...)
}

func TestPruneDryRunAndApplyRemoveOnlyProvenMergedWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fixture := newRepositoryFixture(t)
	branch := "GH-2"
	worktreePath := filepath.Join(home, "worktrees", "example", "repository", branch)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, fixture.primary, "worktree", "add", "-b", branch, worktreePath, "main")
	writeTestFile(t, filepath.Join(worktreePath, "feature.txt"), "feature\n")
	gitTest(t, worktreePath, "add", "feature.txt")
	gitTest(t, worktreePath, "commit", "-m", "feature")
	headOID := gitTest(t, worktreePath, "rev-parse", "HEAD")
	gitTest(t, worktreePath, "push", "-u", "origin", branch)
	gitTest(t, fixture.primary, "merge", "--ff-only", branch)
	gitTest(t, fixture.primary, "push", "origin", "main")
	gitTest(t, fixture.primary, "push", "origin", "--delete", branch)
	mergedAt := time.Now().UTC()
	runner := githubRunner{pullRequests: []PullRequest{{
		Number: 2, State: "MERGED", MergedAt: &mergedAt, BaseRefName: "main",
		HeadRefName: branch, HeadRefOID: headOID, URL: "https://github.com/example/repository/pull/2",
	}}}
	loaded := loadedRepository(t, fixture.root, fixture.primary)
	preview, err := Prune(context.Background(), loaded, Options{Repositories: []string{"api"}, DryRun: true}, runner)
	if err != nil {
		t.Fatal(err)
	}
	decision := findDecision(t, preview, worktreePath)
	registeredPath := decision.Path
	if decision.Action != "would-remove" {
		t.Fatalf("unexpected preview: %#v", decision)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("dry run removed worktree: %v", err)
	}
	report, err := Prune(context.Background(), loaded, Options{Repositories: []string{"api"}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if decision := findDecision(t, report, registeredPath); decision.Action != "removed" {
		t.Fatalf("unexpected removal: %#v", decision)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree remains after prune: %v", err)
	}
}

func TestPrunePreservesDirtyMergedWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fixture := newRepositoryFixture(t)
	branch := "GH-3"
	path := filepath.Join(home, "worktrees", "example", "repository", branch)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, fixture.primary, "worktree", "add", "-b", branch, path, "main")
	writeTestFile(t, filepath.Join(path, "dirty.txt"), "dirty\n")
	loaded := loadedRepository(t, fixture.root, fixture.primary)
	report, err := Prune(context.Background(), loaded, Options{Repositories: []string{"api"}, DryRun: true}, githubRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if decision := findDecision(t, report, path); decision.Reason != "dirty-worktree" || decision.Action != "preserved" {
		t.Fatalf("dirty worktree decision: %#v", decision)
	}
}

func TestPrunePreservesMergedWorktreeWhileRemoteBranchExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fixture := newRepositoryFixture(t)
	branch := "GH-4"
	path := filepath.Join(home, "worktrees", "example", "repository", branch)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, fixture.primary, "worktree", "add", "-b", branch, path, "main")
	writeTestFile(t, filepath.Join(path, "feature.txt"), "feature\n")
	gitTest(t, path, "add", "feature.txt")
	gitTest(t, path, "commit", "-m", "feature")
	headOID := gitTest(t, path, "rev-parse", "HEAD")
	gitTest(t, path, "push", "-u", "origin", branch)
	mergedAt := time.Now().UTC()
	runner := githubRunner{pullRequests: []PullRequest{{
		Number: 4, State: "MERGED", MergedAt: &mergedAt, BaseRefName: "main",
		HeadRefName: branch, HeadRefOID: headOID,
	}}}
	report, err := Prune(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}, DryRun: true}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if decision := findDecision(t, report, path); decision.Action != "preserved" || decision.Reason != "remote-branch-exists" {
		t.Fatalf("remote branch decision = %#v", decision)
	}
}

func TestUnsafePullRequestRequiresSameRepositoryAndExactOID(t *testing.T) {
	mergedAt := time.Now().UTC()
	entry := worktree{Branch: "GH-5", Head: "expected"}
	tests := []struct {
		name    string
		request PullRequest
		reason  string
	}{
		{name: "cross repository", request: PullRequest{State: "MERGED", MergedAt: &mergedAt, BaseRefName: "main", HeadRefOID: "expected", IsCrossRepository: true}, reason: "cross-repository-pull-request"},
		{name: "different oid", request: PullRequest{State: "MERGED", MergedAt: &mergedAt, BaseRefName: "main", HeadRefOID: "different"}, reason: "head-oid-mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := unsafePullRequest(test.request, "main", entry); got != test.reason {
				t.Fatalf("reason = %q, want %q", got, test.reason)
			}
		})
	}
}

func TestPrunePreservesWorktreeUsedByCWDProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fixture := newRepositoryFixture(t)
	path := filepath.Join(home, "worktrees", "example", "repository", "GH-6")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, fixture.primary, "worktree", "add", "-b", "GH-6", path, "main")
	if err := os.Mkdir(filepath.Join(path, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	report, err := Prune(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}, DryRun: true}, worktreeProcessRunner{path: filepath.Join(path, "nested")})
	if err != nil {
		t.Fatal(err)
	}
	decision := findDecision(t, report, path)
	if decision.Action != "preserved" || decision.Reason != "worktree-in-use" || !strings.Contains(decision.Detail, "123") {
		t.Fatalf("in-use decision = %#v", decision)
	}
}

func TestPruneReportsRemovedWorktreeWhenLocalBranchDeletionIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fixture := newRepositoryFixture(t)
	branch := "GH-7"
	path := filepath.Join(home, "worktrees", "example", "repository", branch)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, fixture.primary, "worktree", "add", "-b", branch, path, "main")
	writeTestFile(t, filepath.Join(path, "feature.txt"), "feature\n")
	gitTest(t, path, "add", "feature.txt")
	gitTest(t, path, "commit", "-m", "feature")
	headOID := gitTest(t, path, "rev-parse", "HEAD")
	gitTest(t, path, "push", "-u", "origin", branch)
	gitTest(t, fixture.primary, "push", "origin", "--delete", branch)
	mergedAt := time.Now().UTC()
	runner := githubRunner{pullRequests: []PullRequest{{
		Number: 7, State: "MERGED", MergedAt: &mergedAt, BaseRefName: "main",
		HeadRefName: branch, HeadRefOID: headOID,
	}}}
	registeredPath, err := physicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Prune(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}}, runner)
	if err == nil {
		t.Fatal("preserved local branch did not produce a partial result")
	}
	decision := findDecision(t, report, registeredPath)
	if decision.Action != "removed" || decision.Reason != "worktree-removed-local-branch-preserved" {
		t.Fatalf("partial removal decision = %#v", decision)
	}
	if len(report.Failures) != 1 || report.Failures[0].Operation != "delete-local-branch" {
		t.Fatalf("partial removal failures = %#v", report.Failures)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
	if got := gitTest(t, fixture.primary, "branch", "--list", branch); got == "" {
		t.Fatal("unmerged local branch was deleted")
	}
}

func TestRemoveWorktreeRestoresVerifiedEnvironmentLinkOnFailure(t *testing.T) {
	fixture := newRepositoryFixture(t)
	path := filepath.Join(fixture.root, "feature")
	gitTest(t, fixture.primary, "worktree", "add", "-b", "feature", path, "main")
	writeTestFile(t, filepath.Join(fixture.primary, ".env"), "VALUE=example\n")
	link := filepath.Join(path, ".env")
	if err := os.Symlink(filepath.Join(fixture.primary, ".env"), link); err != nil {
		t.Fatal(err)
	}
	repository := Repository{TopLevel: fixture.primary, Primary: fixture.primary, Remote: "origin"}
	removed, err := removeWorktree(context.Background(), worktreeRemovalFailRunner{}, repository, "main", WorktreeDecision{
		Path: path, Branch: "feature", SafeLinks: []string{link},
	})
	if err == nil || removed {
		t.Fatalf("removal result: removed=%v err=%v", removed, err)
	}
	if !safeEnvironmentLink(fixture.primary, path, ".env") {
		t.Fatal("verified environment link was not restored")
	}
}

func findDecision(t *testing.T, report PruneReport, path string) WorktreeDecision {
	t.Helper()
	if resolved, err := physicalPath(path); err == nil {
		path = resolved
	}
	for _, repository := range report.Repositories {
		for _, decision := range repository.Worktrees {
			if decision.Path == path {
				return decision
			}
		}
	}
	t.Fatalf("missing worktree decision for %s", path)
	return WorktreeDecision{}
}
