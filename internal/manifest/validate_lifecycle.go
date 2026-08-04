package manifest

import "fmt"

func validateLifecycle(root string, lifecycle Lifecycle, add func(string, string)) {
	validateLifecyclePhase(root, "before_up", lifecycle.BeforeUp, add)
	validateLifecyclePhase(root, "after_down", lifecycle.AfterDown, add)
}

func validateLifecyclePhase(
	root string,
	phase string,
	commands []LifecycleCommand,
	add func(string, string),
) {
	names := make(map[string]int, len(commands))
	for index := range commands {
		command := &commands[index]
		prefix := fmt.Sprintf("lifecycle.%s[%d]", phase, index)
		if !serviceNamePattern.MatchString(command.Name) {
			add(prefix+".name", "must match [a-z][a-z0-9-]*")
		}
		if previous, exists := names[command.Name]; exists {
			add(prefix+".name", fmt.Sprintf("duplicates lifecycle.%s[%d]", phase, previous))
		} else {
			names[command.Name] = index
		}
		validateWorkingDirectory(root, command.WorkingDirectory, prefix+".working_directory", "workspace", add)
		validateArgv(command.Run.Argv, prefix+".run.argv", add)
		if command.Timeout.Duration <= 0 {
			add(prefix+".timeout", "must be positive")
		}
		validateEnvironment(root, command.Environment, command.WorkingDirectory, prefix+".environment", "workspace", add)
	}
}
