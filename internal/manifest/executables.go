package manifest

import (
	"path/filepath"
	"strings"
)

// CommandExecutables returns directly executed tools plus executables exposed
// by supported structured wrappers. It never parses shell command strings.
func CommandExecutables(argv []string) []string {
	var result []string
	seen := map[string]bool{}
	current := argv
	for len(current) > 0 {
		if !seen[current[0]] {
			result = append(result, current[0])
			seen[current[0]] = true
		}
		switch filepath.Base(current[0]) {
		case "env":
			index := envCommandIndex(current)
			if index < 0 {
				return result
			}
			current = current[index:]
		case "direnv":
			if len(current) < 4 || current[1] != "exec" {
				return result
			}
			current = current[3:]
		default:
			return result
		}
	}
	return result
}

func envCommandIndex(argv []string) int {
	for index := 1; index < len(argv); index++ {
		value := argv[index]
		switch {
		case value == "--":
			if index+1 < len(argv) {
				return index + 1
			}
			return -1
		case value == "-u" || value == "--unset" || value == "-C" || value == "--chdir":
			index++
		case strings.HasPrefix(value, "--unset=") || strings.HasPrefix(value, "--chdir="):
		case strings.HasPrefix(value, "-"):
		case strings.Contains(value, "="):
		default:
			return index
		}
	}
	return -1
}
