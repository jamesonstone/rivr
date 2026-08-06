package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type defaultState struct {
	Branch    string
	LocalOID  string
	RemoteOID string
	Path      string
	State     string
	Detail    string
}

func liveDefault(ctx context.Context, runner Runner, repository Repository) (string, string, error) {
	content, err := git(ctx, runner, repository.TopLevel, "ls-remote", "--symref", repository.Remote, "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("discover live remote default branch: %w", err)
	}
	branch, oid := parseRemoteHead(content)
	if branch != "" && oid != "" {
		return branch, oid, nil
	}
	if repository.DefaultBranch == "" {
		return "", "", fmt.Errorf("remote did not advertise a symbolic default branch")
	}
	content, err = git(ctx, runner, repository.TopLevel, "ls-remote", "--heads", repository.Remote, "refs/heads/"+repository.DefaultBranch)
	if err != nil {
		return "", "", fmt.Errorf("resolve configured default branch: %w", err)
	}
	fields := strings.Fields(string(content))
	if len(fields) < 2 || fields[1] != "refs/heads/"+repository.DefaultBranch {
		return "", "", fmt.Errorf("configured default branch is absent on remote")
	}
	return repository.DefaultBranch, fields[0], nil
}

func parseRemoteHead(content []byte) (string, string) {
	var branch, oid string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "ref: refs/heads/") && strings.HasSuffix(line, "\tHEAD") {
			branch = strings.TrimSuffix(strings.TrimPrefix(line, "ref: refs/heads/"), "\tHEAD")
		} else if strings.HasSuffix(line, "\tHEAD") && !strings.HasPrefix(line, "ref: ") {
			oid = strings.TrimSuffix(line, "\tHEAD")
		}
	}
	return branch, oid
}

func inspectDefault(ctx context.Context, runner Runner, repository Repository, branch, remoteOID string) defaultState {
	state := defaultState{Branch: branch, RemoteOID: remoteOID, State: "unavailable"}
	localOID, err := gitText(ctx, runner, repository.TopLevel, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		state.State = "missing"
		state.Detail = "local default branch does not exist"
		return state
	}
	state.LocalOID = localOID
	entries, err := listWorktrees(ctx, runner, repository.TopLevel)
	if err != nil {
		state.Detail = "worktree inspection failed"
		return state
	}
	for _, entry := range entries {
		if entry.Branch == branch {
			state.Path = entry.Path
			break
		}
	}
	if localOID == remoteOID {
		state.State = "current"
		return state
	}
	if _, err := git(ctx, runner, repository.TopLevel, "cat-file", "-e", remoteOID+"^{commit}"); err != nil {
		state.State = "remote-object-unavailable"
		state.Detail = "remote commit is not available locally; apply will fetch it"
		return state
	}
	localBehind, localErr := isAncestor(ctx, runner, repository.TopLevel, localOID, remoteOID)
	remoteBehind, remoteErr := isAncestor(ctx, runner, repository.TopLevel, remoteOID, localOID)
	if localErr != nil || remoteErr != nil {
		state.Detail = "default-branch ancestry could not be classified"
		return state
	}
	switch {
	case localBehind && !remoteBehind:
		state.State = "behind"
	case remoteBehind && !localBehind:
		state.State = "ahead"
	case !localBehind && !remoteBehind:
		state.State = "diverged"
	default:
		state.Detail = "default-branch ancestry is ambiguous"
	}
	return state
}

func isAncestor(ctx context.Context, runner Runner, directory, ancestor, descendant string) (bool, error) {
	_, err := git(ctx, runner, directory, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func cleanWorktree(ctx context.Context, runner Runner, directory string) (bool, error) {
	content, err := git(ctx, runner, directory, "status", "--porcelain", "--untracked-files=all")
	return len(strings.TrimSpace(string(content))) == 0, err
}
