package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var helpANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRootHelpMatchesPlainGolden(t *testing.T) {
	setTerminalHelp(t, false)
	t.Setenv("NO_COLOR", "")
	output := executeHelp(t, "--help")
	want, err := os.ReadFile(filepath.Join("testdata", "root-help.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if output != string(want) {
		t.Fatalf("plain root help changed\n--- want ---\n%s\n--- got ---\n%s", want, output)
	}
	if helpCommand := executeHelp(t, "help"); helpCommand != output {
		t.Fatal("rungrid help differs from rungrid --help")
	}
}

func TestRootHelpUsesColorForTerminalOutput(t *testing.T) {
	setTerminalHelp(t, true)
	t.Setenv("NO_COLOR", "")
	output := executeHelp(t, "--help")
	if !helpANSIPattern.MatchString(output) {
		t.Fatalf("terminal root help did not contain ANSI styling: %q", output)
	}
	plain := helpANSIPattern.ReplaceAllString(output, "")
	for _, expected := range []string{
		"one workspace. truthful lifecycle.",
		"🔁 Workspace Lifecycle",
		"Process Compose",
		"🧩 Service Ownership",
		"🚀 Usage",
		"🧰 Available Commands",
		"⚙️ Flags",
		"🔎 Use",
	} {
		if !strings.Contains(plain, expected) {
			t.Errorf("colored root help missing %q", expected)
		}
	}
}

func TestRootHelpColorCanBeDisabled(t *testing.T) {
	setTerminalHelp(t, true)
	for _, test := range []struct {
		name    string
		noColor string
		args    []string
	}{
		{name: "flag", args: []string{"--no-color", "--help"}},
		{name: "environment", noColor: "1", args: []string{"--help"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", test.noColor)
			if output := executeHelp(t, test.args...); helpANSIPattern.MatchString(output) {
				t.Fatalf("color-disabled root help contained ANSI styling: %q", output)
			}
		})
	}
}

func TestSubcommandHelpUsesSharedPresentation(t *testing.T) {
	setTerminalHelp(t, true)
	t.Setenv("NO_COLOR", "")
	colored := executeHelp(t, "instructions", "--help")
	if !helpANSIPattern.MatchString(colored) {
		t.Fatalf("terminal subcommand help did not contain ANSI styling: %q", colored)
	}
	plain := helpANSIPattern.ReplaceAllString(colored, "")
	for _, expected := range []string{
		"🚀 Usage",
		"🏷️ Aliases",
		"instructions, agent-start",
		"🧪 Examples",
		"⚙️ Flags",
		"🌐 Global Flags",
	} {
		if !strings.Contains(plain, expected) {
			t.Errorf("colored subcommand help missing %q", expected)
		}
	}
	if helpCommand := executeHelp(t, "help", "instructions"); helpCommand != colored {
		t.Fatal("rungrid help instructions differs from rungrid instructions --help")
	}

	colorless := executeHelp(t, "--no-color", "instructions", "--help")
	if helpANSIPattern.MatchString(colorless) {
		t.Fatalf("color-disabled subcommand help contained ANSI styling: %q", colorless)
	}
	for _, expected := range []string{"Usage:", "Aliases:", "Examples:", "Flags:", "Global Flags:"} {
		if !strings.Contains(colorless, expected) {
			t.Errorf("plain subcommand help missing %q", expected)
		}
	}
}

func TestEveryVisibleRootCommandIsGroupedOnce(t *testing.T) {
	root := newRootCommand()
	root.InitDefaultHelpCmd()
	counts := map[string]int{}
	for _, section := range rootHelpSections {
		for _, name := range section.commands {
			counts[name]++
		}
	}
	for _, command := range root.Commands() {
		if command.Hidden || !command.IsAvailableCommand() {
			continue
		}
		if counts[command.Name()] != 1 {
			t.Errorf("visible command %q is grouped %d times", command.Name(), counts[command.Name()])
		}
	}
	for name, count := range counts {
		if count != 1 || visibleSubcommand(root, name) == nil {
			t.Errorf("help group entry %q has count %d and no visible command", name, count)
		}
	}
}

func executeHelp(t *testing.T, args ...string) string {
	t.Helper()
	root := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func setTerminalHelp(t *testing.T, enabled bool) {
	t.Helper()
	previous := terminalWriterCheck
	terminalWriterCheck = func(_ io.Writer) bool { return enabled }
	t.Cleanup(func() { terminalWriterCheck = previous })
}
