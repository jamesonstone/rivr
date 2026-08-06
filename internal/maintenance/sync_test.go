package maintenance

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type recordingCoordinator struct {
	paths       []string
	pauseCount  int
	resumeCount int
	resumeErr   error
}

func (coordinator *recordingCoordinator) AffectedServices(_ context.Context, path string) ([]string, error) {
	coordinator.paths = append(coordinator.paths, path)
	return []string{"api"}, nil
}

func (coordinator *recordingCoordinator) Pause(_ context.Context, path string) ([]string, ResumeFunc, error) {
	coordinator.paths = append(coordinator.paths, path)
	coordinator.pauseCount++
	return []string{"api"}, func(context.Context) error {
		coordinator.resumeCount++
		return coordinator.resumeErr
	}, nil
}

type mutatingCoordinator struct {
	t *testing.T
}

type cancelingCoordinator struct {
	cancel       context.CancelFunc
	resumeCalled bool
	resumeErr    error
}

func (coordinator *cancelingCoordinator) AffectedServices(context.Context, string) ([]string, error) {
	return []string{"api"}, nil
}

func (coordinator *cancelingCoordinator) Pause(context.Context, string) ([]string, ResumeFunc, error) {
	coordinator.cancel()
	return []string{"api"}, func(ctx context.Context) error {
		coordinator.resumeCalled = true
		coordinator.resumeErr = ctx.Err()
		return coordinator.resumeErr
	}, nil
}

func (coordinator mutatingCoordinator) AffectedServices(context.Context, string) ([]string, error) {
	return []string{"api"}, nil
}

func (coordinator mutatingCoordinator) Pause(_ context.Context, path string) ([]string, ResumeFunc, error) {
	writeTestFile(coordinator.t, filepath.Join(path, "concurrent.txt"), "concurrent\n")
	gitTest(coordinator.t, path, "add", "concurrent.txt")
	gitTest(coordinator.t, path, "commit", "-m", "concurrent")
	return []string{"api"}, func(context.Context) error { return nil }, nil
}

func TestSyncDryRunDoesNotFetchOrAdvanceDefault(t *testing.T) {
	fixture := newRepositoryFixture(t)
	remoteOID := fixture.advanceRemote(t, "remote")
	localOID := gitTest(t, fixture.primary, "rev-parse", "main")
	loaded := loadedRepository(t, fixture.root, fixture.primary)
	report, err := Sync(context.Background(), loaded, Options{Repositories: []string{"api"}, DryRun: true}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := report.Repositories[0]
	if result.Action != "would-fetch" || result.RemoteOID != remoteOID {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	if got := gitTest(t, fixture.primary, "rev-parse", "main"); got != localOID {
		t.Fatalf("dry run advanced main to %s", got)
	}
	if got := gitTest(t, fixture.primary, "rev-parse", "refs/remotes/origin/main"); got != localOID {
		t.Fatalf("dry run fetched origin/main to %s", got)
	}
}

func TestSyncAdvancesDefaultAndCoordinatesItsRunningServices(t *testing.T) {
	fixture := newRepositoryFixture(t)
	remoteOID := fixture.advanceRemote(t, "remote")
	loaded := loadedRepository(t, fixture.root, fixture.primary)
	coordinator := &recordingCoordinator{}
	report, err := Sync(context.Background(), loaded, Options{Repositories: []string{"api"}}, nil, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitTest(t, fixture.primary, "rev-parse", "main"); got != remoteOID {
		t.Fatalf("main = %s, want %s", got, remoteOID)
	}
	if coordinator.pauseCount != 1 || coordinator.resumeCount != 1 || len(coordinator.paths) != 2 {
		t.Fatalf("unexpected coordination: %#v", coordinator)
	}
	primary, _ := physicalPath(fixture.primary)
	if coordinator.paths[0] != primary {
		t.Fatalf("coordinated path = %q", coordinator.paths[0])
	}
	if result := report.Repositories[0]; result.Action != "fast-forwarded" || result.State != "current" {
		t.Fatalf("unexpected sync result: %#v", result)
	}
}

func TestSyncLeavesActiveFeatureWorktreeUntouched(t *testing.T) {
	fixture := newRepositoryFixture(t)
	feature := filepath.Join(fixture.root, "feature")
	gitTest(t, fixture.primary, "worktree", "add", "-b", "feature", feature, "main")
	featureOID := gitTest(t, feature, "rev-parse", "HEAD")
	fixture.advanceRemote(t, "remote")
	loaded := loadedRepository(t, fixture.root, feature)
	coordinator := &recordingCoordinator{}
	if _, err := Sync(context.Background(), loaded, Options{Repositories: []string{"api"}}, nil, coordinator); err != nil {
		t.Fatal(err)
	}
	if got := gitTest(t, feature, "branch", "--show-current"); got != "feature" {
		t.Fatalf("feature worktree switched to %q", got)
	}
	if got := gitTest(t, feature, "rev-parse", "HEAD"); got != featureOID {
		t.Fatalf("feature HEAD changed to %s", got)
	}
	primary, _ := physicalPath(fixture.primary)
	if coordinator.paths[0] != primary {
		t.Fatalf("feature worktree was coordinated: %#v", coordinator.paths)
	}
}

func TestSyncPreservesDirtyDefaultWorktree(t *testing.T) {
	fixture := newRepositoryFixture(t)
	fixture.advanceRemote(t, "remote")
	writeTestFile(t, filepath.Join(fixture.primary, "local.txt"), "local\n")
	coordinator := &recordingCoordinator{}
	report, err := Sync(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}}, nil, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	result := report.Repositories[0]
	if result.State != "dirty" || result.Action != "preserved" || coordinator.pauseCount != 0 {
		t.Fatalf("dirty default result = %#v, coordinator = %#v", result, coordinator)
	}
}

func TestSyncPreservesDivergedDefaultBranch(t *testing.T) {
	fixture := newRepositoryFixture(t)
	fixture.advanceRemote(t, "remote")
	writeTestFile(t, filepath.Join(fixture.primary, "local.txt"), "local\n")
	gitTest(t, fixture.primary, "add", "local.txt")
	gitTest(t, fixture.primary, "commit", "-m", "local")
	localOID := gitTest(t, fixture.primary, "rev-parse", "main")
	coordinator := &recordingCoordinator{}
	report, err := Sync(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}}, nil, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	result := report.Repositories[0]
	if result.State != "diverged" || result.Action != "preserved" || coordinator.pauseCount != 0 {
		t.Fatalf("diverged default result = %#v, coordinator = %#v", result, coordinator)
	}
	if got := gitTest(t, fixture.primary, "rev-parse", "main"); got != localOID {
		t.Fatalf("diverged main changed from %s to %s", localOID, got)
	}
}

func TestSyncRevalidatesDefaultOIDAfterPausingServices(t *testing.T) {
	fixture := newRepositoryFixture(t)
	remoteOID := fixture.advanceRemote(t, "remote")
	report, err := Sync(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}}, nil, mutatingCoordinator{t: t})
	if err == nil {
		t.Fatal("concurrent default-branch change was accepted")
	}
	result := report.Repositories[0]
	if result.Action != "failed" || !strings.Contains(result.Detail, "OID changed") {
		t.Fatalf("concurrent result = %#v", result)
	}
	if got := gitTest(t, fixture.primary, "rev-parse", "main"); got == remoteOID {
		t.Fatal("sync overwrote the concurrent local commit")
	}
}

func TestSyncReportsCompletedUpdateWhenServiceResumeFails(t *testing.T) {
	fixture := newRepositoryFixture(t)
	remoteOID := fixture.advanceRemote(t, "remote")
	coordinator := &recordingCoordinator{resumeErr: errors.New("restart failed")}
	report, err := Sync(context.Background(), loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}}, nil, coordinator)
	if err == nil {
		t.Fatal("resume failure did not produce a partial result")
	}
	result := report.Repositories[0]
	if result.Action != "fast-forwarded" || result.State != "current" || result.LocalOID != remoteOID || !strings.Contains(result.Detail, "could not be resumed") {
		t.Fatalf("resume failure result = %#v", result)
	}
}

func TestSyncUsesRecoveryContextAfterCallerCancellation(t *testing.T) {
	fixture := newRepositoryFixture(t)
	fixture.advanceRemote(t, "remote")
	ctx, cancel := context.WithCancel(context.Background())
	coordinator := &cancelingCoordinator{cancel: cancel}
	report, err := Sync(ctx, loadedRepository(t, fixture.root, fixture.primary),
		Options{Repositories: []string{"api"}}, nil, coordinator)
	if err == nil {
		t.Fatal("canceled update unexpectedly succeeded")
	}
	if !coordinator.resumeCalled || coordinator.resumeErr != nil {
		t.Fatalf("recovery context was canceled: coordinator=%#v", coordinator)
	}
	if len(report.Failures) != 1 || report.Failures[0].Operation != "fast-forward" {
		t.Fatalf("canceled update failures = %#v", report.Failures)
	}
}
