package manifest

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func validateWorkingDirectory(root, directory, field, scope string, add func(string, string)) {
	if filepath.IsAbs(directory) {
		add(field, "must be "+scope+"-relative")
		return
	}
	clean := filepath.Clean(directory)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		add(field, "must remain within the "+scope)
		return
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		add(field, "must name an existing directory")
		return
	}
	if !within(root, resolved) {
		add(field, "resolves outside the "+scope)
		return
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		add(field, "must name a directory")
	}
}

func validateCompose(root string, service *Service, prefix string, add func(string, string)) {
	compose := service.Compose
	if filepath.IsAbs(compose.File) {
		add(prefix+".compose.file", "must be repository-relative")
	} else {
		filename := filepath.Join(root, service.WorkingDirectory, compose.File)
		resolved, err := filepath.EvalSymlinks(filename)
		if err != nil || !within(root, resolved) {
			add(prefix+".compose.file", "must name a file within the repository")
		} else if info, statErr := os.Stat(resolved); statErr != nil || info.IsDir() {
			add(prefix+".compose.file", "must name an existing file")
		}
	}
	if strings.TrimSpace(compose.Service) == "" {
		add(prefix+".compose.service", "is required")
	}
	validateArgv(compose.UpArgv, prefix+".compose.up_argv", add)
	validateArgv(compose.DownArgv, prefix+".compose.down_argv", add)
}

func validateExternal(external *External, prefix string, add func(string, string)) {
	if external.URL == "" && external.Command == nil {
		add(prefix, "must define url or command")
	}
	if external.URL != "" {
		parsed, err := url.Parse(external.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			add(prefix+".url", "must be an http or https URL with a host")
		}
	}
	if external.Command != nil {
		validateArgv(external.Command.Argv, prefix+".command.argv", add)
	}
}

func validateEnvironment(
	root string,
	environment Environment,
	workingDirectory string,
	prefix string,
	scope string,
	add func(string, string),
) {
	for key := range environment.Values {
		if secretKeyPattern.MatchString(key) {
			add(prefix+".values."+key, "secret-like keys must use an execution-time environment provider")
		}
	}
	for i, provider := range environment.Providers {
		field := fmt.Sprintf("%s.providers[%d]", prefix, i)
		switch provider.Type {
		case "dotenv":
			if provider.Path == "" {
				add(field+".path", "is required")
			} else if filepath.IsAbs(provider.Path) {
				add(field+".path", "must be "+scope+"-relative")
			} else {
				candidate := filepath.Join(root, workingDirectory, provider.Path)
				if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
					if !within(root, resolved) {
						add(field+".path", "resolves outside the "+scope)
					}
				} else if !provider.Optional {
					add(field+".path", "must exist unless optional")
				}
			}
		case "command":
			validateArgv(provider.Argv, field+".argv", add)
			if provider.Timeout.Duration <= 0 {
				add(field+".timeout", "must be positive")
			}
		case "direnv":
			if provider.Directory == "" {
				add(field+".directory", "is required")
			} else {
				validateWorkingDirectory(root, filepath.Join(workingDirectory, provider.Directory), field+".directory", scope, add)
			}
		default:
			add(field+".type", "must be dotenv, command, or direnv")
		}
	}
}

func validateHealth(health *Health, prefix string, add func(string, string)) {
	if health == nil {
		return
	}
	if health.Command == nil && health.URL == "" {
		add(prefix, "must define command or url")
	}
	if health.Command != nil && health.URL != "" {
		add(prefix, "must define only one of command or url")
	}
	if health.Command != nil {
		validateArgv(health.Command.Argv, prefix+".command.argv", add)
	}
	if health.URL != "" {
		parsed, err := url.Parse(health.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			add(prefix+".url", "must be an http or https URL with a host")
		}
	}
	if health.Interval.Duration <= 0 {
		add(prefix+".interval", "must be positive")
	}
	if health.Timeout.Duration <= 0 {
		add(prefix+".timeout", "must be positive")
	}
	if health.Retries <= 0 {
		add(prefix+".retries", "must be positive")
	}
	if health.StartPeriod.Duration < 0 {
		add(prefix+".start_period", "must not be negative")
	}
}

func validateArgv(argv []string, field string, add func(string, string)) {
	if len(argv) == 0 {
		add(field, "must be a non-empty argument vector")
		return
	}
	for i, value := range argv {
		if value == "" {
			add(fmt.Sprintf("%s[%d]", field, i), "must not be empty")
		}
		if strings.ContainsRune(value, '\x00') {
			add(fmt.Sprintf("%s[%d]", field, i), "must not contain NUL")
		}
	}
}

func dependencyCycle(services []Service) []string {
	graph := make(map[string][]string, len(services))
	for _, service := range services {
		for dependency := range service.DependsOn {
			graph[service.Name] = append(graph[service.Name], dependency)
		}
		sort.Strings(graph[service.Name])
	}
	state := map[string]int{}
	stack := []string{}
	var visit func(string) []string
	visit = func(name string) []string {
		state[name] = 1
		stack = append(stack, name)
		for _, dependency := range graph[name] {
			switch state[dependency] {
			case 0:
				if cycle := visit(dependency); len(cycle) > 0 {
					return cycle
				}
			case 1:
				for i, value := range stack {
					if value == dependency {
						return append(append([]string(nil), stack[i:]...), dependency)
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, service := range services {
		if state[service.Name] == 0 {
			if cycle := visit(service.Name); len(cycle) > 0 {
				return cycle
			}
		}
	}
	return nil
}

func FindService(m *Manifest, name string) (*Service, bool) {
	for i := range m.Services {
		if m.Services[i].Name == name {
			return &m.Services[i], true
		}
	}
	return nil, false
}
