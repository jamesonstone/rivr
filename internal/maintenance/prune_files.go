package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func worktreeCleanliness(ctx context.Context, runner Runner, repository Repository, path string) ([]string, bool, error) {
	status, err := git(ctx, runner, path, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, false, err
	}
	ignored, err := git(ctx, runner, path, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil, false, err
	}
	allowed := make(map[string]bool)
	var safeLinks []string
	for _, name := range []string{".env", ".envrc"} {
		if safeEnvironmentLink(repository.Primary, path, name) {
			allowed[name] = true
			safeLinks = append(safeLinks, filepath.Join(path, name))
		}
	}
	for _, record := range splitNUL(status) {
		if len(record) < 4 || record[:2] != "??" || !allowed[record[3:]] {
			return safeLinks, false, nil
		}
	}
	for _, name := range splitNUL(ignored) {
		if !allowed[name] {
			return safeLinks, false, nil
		}
	}
	return safeLinks, true, nil
}

func safeEnvironmentLink(primary, worktreePath, name string) bool {
	destination := filepath.Join(worktreePath, name)
	info, err := os.Lstat(destination)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	source := filepath.Join(primary, name)
	if sourceInfo, sourceErr := os.Stat(source); sourceErr != nil || sourceInfo.IsDir() {
		return false
	}
	target, err := os.Readlink(destination)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(destination), target)
	}
	resolvedTarget, targetErr := filepath.EvalSymlinks(target)
	resolvedSource, sourceErr := filepath.EvalSymlinks(source)
	return targetErr == nil && sourceErr == nil && resolvedTarget == resolvedSource
}

func splitNUL(content []byte) []string {
	parts := strings.Split(string(content), "\x00")
	result := parts[:0]
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

type removedLink struct {
	path   string
	target string
}

func removeWorktree(ctx context.Context, runner Runner, repository Repository, defaultBranch string, decision WorktreeDecision) (bool, error) {
	removed, err := removeSafeLinks(repository, decision)
	if err != nil {
		return false, err
	}
	if _, err := git(ctx, runner, repository.TopLevel, "worktree", "remove", decision.Path); err != nil {
		return false, errors.Join(err, restoreLinks(removed))
	}
	_, err = git(ctx, runner, repository.TopLevel,
		"-c", "branch."+decision.Branch+".remote="+repository.Remote,
		"-c", "branch."+decision.Branch+".merge=refs/heads/"+defaultBranch,
		"branch", "-d", "--", decision.Branch)
	if err != nil {
		return true, fmt.Errorf("worktree removed but local branch was preserved: %w", err)
	}
	return true, nil
}

func removeSafeLinks(repository Repository, decision WorktreeDecision) ([]removedLink, error) {
	var removed []removedLink
	for _, path := range decision.SafeLinks {
		name := filepath.Base(path)
		if !safeEnvironmentLink(repository.Primary, decision.Path, name) {
			return nil, errors.Join(fmt.Errorf("environment link changed before removal: %s", path), restoreLinks(removed))
		}
		target, err := os.Readlink(path)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("read verified environment link %s: %w", path, err), restoreLinks(removed))
		}
		if err := os.Remove(path); err != nil {
			return nil, errors.Join(fmt.Errorf("remove verified environment link %s: %w", path, err), restoreLinks(removed))
		}
		removed = append(removed, removedLink{path: path, target: target})
	}
	return removed, nil
}

func restoreLinks(links []removedLink) error {
	var result error
	for _, link := range links {
		if _, err := os.Lstat(link.path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			result = errors.Join(result, fmt.Errorf("inspect environment link restoration %s: %w", link.path, err))
			continue
		}
		if err := os.Symlink(link.target, link.path); err != nil {
			result = errors.Join(result, fmt.Errorf("restore environment link %s: %w", link.path, err))
		}
	}
	return result
}
