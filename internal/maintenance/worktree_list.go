package maintenance

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func listWorktrees(ctx context.Context, runner Runner, directory string) ([]worktree, error) {
	content, err := git(ctx, runner, directory, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var result []worktree
	var current *worktree
	for _, line := range strings.Split(string(content), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path, pathErr := filepath.Abs(strings.TrimPrefix(line, "worktree "))
			if pathErr != nil {
				return nil, fmt.Errorf("resolve registered worktree: %w", pathErr)
			}
			path = filepath.Clean(path)
			if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
				path = resolved
			}
			result = append(result, worktree{Path: path})
			current = &result[len(result)-1]
		case current == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "detached":
			current.Detached = true
		case strings.HasPrefix(line, "locked"):
			current.Locked = true
		case strings.HasPrefix(line, "prunable"):
			current.Prunable = true
		}
	}
	return result, nil
}
