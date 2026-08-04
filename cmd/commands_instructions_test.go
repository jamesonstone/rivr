package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/output"
)

func TestInstructionsAndAgentStartAreEquivalent(t *testing.T) {
	t.Parallel()
	canonical := executeInstructions(t, "instructions", "../api", "../web")
	alias := executeInstructions(t, "agent-start", "../api", "../web")
	if canonical != alias {
		t.Fatal("agent-start output differs from instructions output")
	}
	for _, expected := range []string{"# Rungrid workspace wiring task", "\"../api\"", "rungrid config validate"} {
		if !strings.Contains(canonical, expected) {
			t.Errorf("instructions output missing %q", expected)
		}
	}
}

func TestInstructionsJSONEnvelope(t *testing.T) {
	t.Parallel()
	root := newRootCommand()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--json", "instructions", "../api"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.APIVersion != output.APIVersion || envelope.Kind != "AgentInstructions" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok || data["canonical_command"] != "instructions" || data["alias"] != "agent-start" {
		t.Fatalf("unexpected instructions data: %#v", envelope.Data)
	}
}

func TestRootHelpNamesAgentStartAlias(t *testing.T) {
	t.Parallel()
	root := newRootCommand()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"instructions", "alias: agent-start"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("root help missing %q", expected)
		}
	}
}

func executeInstructions(t *testing.T, args ...string) string {
	t.Helper()
	root := newRootCommand()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return stdout.String()
}
