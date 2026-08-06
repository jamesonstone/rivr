package maintenance

import (
	"fmt"
	"io"
	"strings"
)

func WriteSyncHuman(writer io.Writer, report SyncReport) error {
	if _, err := fmt.Fprintln(writer, "REPOSITORY  DEFAULT  LOCAL     REMOTE    SERVICES  RESULT"); err != nil {
		return err
	}
	for _, repository := range report.Repositories {
		result := repository.Action
		if repository.Detail != "" && repository.Action == "preserved" {
			result = repository.State + ": " + repository.Detail
		} else if repository.Action == "preserved" {
			result = repository.State
		}
		if _, err := fmt.Fprintf(writer, "%-11s %-8s %-9s %-9s %-9s %s\n",
			repository.Name, emptyDash(repository.DefaultBranch), shortOID(repository.LocalOID),
			shortOID(repository.RemoteOID), emptyDash(strings.Join(repository.Services, ",")), result); err != nil {
			return err
		}
	}
	for _, failure := range report.Failures {
		if _, err := fmt.Fprintf(writer, "warning: %s %s: %s\n", failure.Repository, failure.Operation, failure.Error); err != nil {
			return err
		}
	}
	return nil
}

func WritePruneHuman(writer io.Writer, report PruneReport) error {
	if _, err := fmt.Fprintln(writer, "REPOSITORY  WORKTREE  BRANCH  PR  ACTION  REASON"); err != nil {
		return err
	}
	for _, repository := range report.Repositories {
		for _, decision := range repository.Worktrees {
			pullRequest := "-"
			if decision.PullRequest != nil {
				pullRequest = fmt.Sprintf("#%d", decision.PullRequest.Number)
			}
			if _, err := fmt.Fprintf(writer, "%-11s %-9s %-7s %-3s %-7s %s\n",
				repository.Name, decision.Path, emptyDash(decision.Branch), pullRequest,
				decision.Action, decision.Reason); err != nil {
				return err
			}
		}
	}
	for _, failure := range report.Failures {
		if _, err := fmt.Fprintf(writer, "warning: %s %s: %s\n", failure.Repository, failure.Operation, failure.Error); err != nil {
			return err
		}
	}
	return nil
}

func RemovalCount(report PruneReport) int {
	count := 0
	for _, repository := range report.Repositories {
		for _, decision := range repository.Worktrees {
			if decision.Action == "remove" || decision.Action == "would-remove" {
				count++
			}
		}
	}
	return count
}

func shortOID(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	return emptyDash(value)
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
