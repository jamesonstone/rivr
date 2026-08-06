package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func evaluateWorktree(ctx context.Context, runner Runner, repository Repository, defaultBranch string, entry worktree, primary bool) (WorktreeDecision, error) {
	decision := WorktreeDecision{Repository: repository.Name, Path: entry.Path, Branch: entry.Branch, HeadOID: entry.Head, Action: "preserved"}
	switch {
	case primary || entry.Path == repository.Primary:
		decision.Reason = "primary-worktree"
	case entry.Prunable:
		decision.Reason = "stale-metadata"
	case entry.Detached || entry.Branch == "":
		decision.Reason = "detached-worktree"
	case entry.Locked:
		decision.Reason = "locked-worktree"
	case entry.Branch == defaultBranch:
		decision.Reason = "default-branch-worktree"
	case containsPath(repository.DeclaredPaths, entry.Path):
		decision.Reason = "manifest-declared-worktree"
	case isInternalWorktree(entry.Path):
		decision.Reason = "internal-worktree"
	case !isCanonicalWorktree(repository, entry.Path):
		decision.Reason = "non-canonical-worktree"
	default:
		return inspectRemovalCandidate(ctx, runner, repository, defaultBranch, entry, decision)
	}
	return decision, nil
}

func inspectRemovalCandidate(ctx context.Context, runner Runner, repository Repository, defaultBranch string, entry worktree, decision WorktreeDecision) (WorktreeDecision, error) {
	safeLinks, clean, err := worktreeCleanliness(ctx, runner, repository, entry.Path)
	if err != nil {
		decision.Reason = "inspection-failed"
		return decision, err
	}
	if !clean {
		decision.Reason = "dirty-worktree"
		return decision, nil
	}
	decision.SafeLinks = safeLinks
	if repository.RemoteSlug == "" {
		decision.Reason = "unsupported-remote"
		return decision, nil
	}
	processes, err := worktreeProcesses(ctx, runner, repository.TopLevel, entry.Path)
	if err != nil {
		decision.Reason = "process-inspection-failed"
		return decision, err
	}
	if len(processes) != 0 {
		decision.Reason = "worktree-in-use"
		decision.Detail = "cwd process ids: " + strings.Join(processes, ",")
		return decision, nil
	}
	requests, err := pullRequests(ctx, runner, repository, entry.Branch)
	if err != nil {
		decision.Reason = "github-unavailable"
		return decision, err
	}
	if len(requests) != 1 {
		decision.Reason = "pull-request-missing"
		if len(requests) > 1 {
			decision.Reason = "pull-request-ambiguous"
		}
		return decision, nil
	}
	request := requests[0]
	decision.PullRequest = &request
	if reason := unsafePullRequest(request, defaultBranch, entry); reason != "" {
		decision.Reason = reason
		return decision, nil
	}
	remoteHead, err := git(ctx, runner, repository.TopLevel, "ls-remote", "--heads", repository.Remote, "refs/heads/"+entry.Branch)
	if err != nil {
		decision.Reason = "remote-branch-unavailable"
		return decision, err
	}
	if strings.TrimSpace(string(remoteHead)) != "" {
		decision.Reason = "remote-branch-exists"
		return decision, nil
	}
	decision.Action, decision.Reason = "remove", "merged-pr-remote-deleted"
	return decision, nil
}

func pullRequests(ctx context.Context, runner Runner, repository Repository, branch string) ([]PullRequest, error) {
	content, err := runner.Run(ctx, repository.TopLevel, "gh", "pr", "list", "--repo", repository.RemoteSlug,
		"--state", "all", "--limit", "100", "--head", branch, "--json",
		"number,state,mergedAt,baseRefName,headRefName,headRefOid,isCrossRepository,url")
	if err != nil {
		return nil, err
	}
	var result []PullRequest
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, fmt.Errorf("decode pull requests: %w", err)
	}
	filtered := result[:0]
	for _, request := range result {
		if request.HeadRefName == branch {
			filtered = append(filtered, request)
		}
	}
	return filtered, nil
}

func unsafePullRequest(request PullRequest, defaultBranch string, entry worktree) string {
	switch {
	case request.IsCrossRepository:
		return "cross-repository-pull-request"
	case !strings.EqualFold(request.State, "MERGED") || request.MergedAt == nil:
		if strings.EqualFold(request.State, "OPEN") {
			return "pull-request-open"
		}
		return "pull-request-not-merged"
	case request.BaseRefName != defaultBranch:
		return "pull-request-wrong-base"
	case request.HeadRefOID == "" || request.HeadRefOID != entry.Head:
		return "head-oid-mismatch"
	default:
		return ""
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if filepath.Clean(path) == filepath.Clean(target) {
			return true
		}
	}
	return false
}

func isInternalWorktree(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	return strings.Contains(normalized, "/.codex/worktrees/")
}

func isCanonicalWorktree(repository Repository, path string) bool {
	root := canonicalRoot(repository)
	if root == "" {
		return false
	}
	if resolved, err := physicalPath(root); err == nil {
		root = resolved
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func worktreeProcesses(ctx context.Context, runner Runner, directory, worktreePath string) ([]string, error) {
	content, err := runner.Run(ctx, directory, "lsof", "-a", "-d", "cwd", "-Fn")
	if err != nil {
		return nil, fmt.Errorf("inspect cwd processes: %w", err)
	}
	var currentPID string
	var result []string
	for _, line := range strings.Split(string(content), "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			currentPID = strings.TrimPrefix(line, "p")
		case strings.HasPrefix(line, "n") && currentPID != "":
			if pathInside(worktreePath, strings.TrimPrefix(line, "n")) {
				result = append(result, currentPID)
			}
		}
	}
	return result, nil
}

func pathInside(root, candidate string) bool {
	if resolved, err := physicalPath(root); err == nil {
		root = resolved
	}
	if resolved, err := physicalPath(candidate); err == nil {
		candidate = resolved
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}
