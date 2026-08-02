package cmd

import "testing"

func TestRootExposesV1Commands(t *testing.T) {
	t.Parallel()
	root := newRootCommand()
	commands := map[string]bool{}
	for _, command := range root.Commands() {
		if !command.Hidden {
			commands[command.Name()] = true
		}
	}
	for _, expected := range []string{
		"init", "doctor", "plan", "generate", "up", "open", "attach", "versions",
		"status", "logs", "session", "start", "stop", "down", "uninstall", "config",
		"completion", "version",
	} {
		if !commands[expected] {
			t.Errorf("missing command %q", expected)
		}
	}
}
