package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCLISpecIsNeutral(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("CLI_SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(content))
	for _, forbidden := range []string{
		"jamesonstone",
		"labcore",
		"labcore-ui",
		"flowcore",
		"lsmc",
		"/users/",
		"github.com/",
		"homebrew tap",
		"brew tap",
		"brew install --cask",
	} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("CLI_SPEC.md contains forbidden project-specific text %q", forbidden)
		}
	}
	absolutePath := regexp.MustCompile(`(?m)(?:^|[[:space:]\x60'\"])/(?:[A-Za-z0-9._-]+/)+[A-Za-z0-9._-]+`)
	for _, match := range absolutePath.FindAllString(string(content), -1) {
		trimmed := strings.TrimSpace(strings.TrimLeft(match, "`'\""))
		if strings.HasPrefix(trimmed, "/path/to/") || strings.HasPrefix(trimmed, "/tmp/") {
			continue
		}
		t.Errorf("CLI_SPEC.md contains non-placeholder absolute path %q", trimmed)
	}
}

func TestCLISpecDefinesV1Contract(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("CLI_SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"rungrid/v1",
		"rungrid/output/v1",
		"**Overview**",
		"**Versions**",
		"`workspace` services",
		"`tab` services",
		"Process Compose `>=1.120.0,<2.0.0`",
		"Warp is the only graphical terminal adapter in v1",
		"workspace.root",
		"lifecycle.before_up",
		"cleanup-required",
		"Starting or stopping one service never runs global lifecycle hooks",
		"## 15. Legacy-workspace migration contract",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("CLI_SPEC.md is missing required contract text %q", required)
		}
	}
}
