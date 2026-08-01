package manifest

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
)

var (
	serviceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	slugPattern        = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	projectIDPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*-[a-z2-7]{6}$`)
	triggerNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	secretKeyPattern   = regexp.MustCompile(`(?i)(secret|token|password|passwd|private[_-]?key|api[_-]?key|credential)`)
)

func Validate(m *Manifest, root string) error {
	var problems []string
	add := func(path, message string) { problems = append(problems, path+": "+message) }

	if m.APIVersion != APIVersion {
		add("api_version", fmt.Sprintf("must be %q", APIVersion))
	}
	if m.Kind != Kind {
		add("kind", fmt.Sprintf("must be %q", Kind))
	}
	if strings.TrimSpace(m.Project.Name) == "" {
		add("project.name", "is required")
	}
	if !slugPattern.MatchString(m.Project.Slug) {
		add("project.slug", "must contain lowercase letters, digits, and single hyphens")
	}
	if m.Project.ID == "" {
		add("project.id", "is required for stable project-scoped state; run rungrid init")
	} else if !projectIDPattern.MatchString(m.Project.ID) || !strings.HasPrefix(m.Project.ID, m.Project.Slug+"-") {
		add("project.id", "must be the project slug plus a six-character lowercase base32 suffix")
	}
	if m.Runtime.StartupTimeout.Duration <= 0 {
		add("runtime.startup_timeout", "must be positive")
	}
	if m.Runtime.ShutdownTimeout.Duration <= 0 {
		add("runtime.shutdown_timeout", "must be positive")
	}
	if m.Runtime.LogRetention.Duration <= 0 {
		add("runtime.log_retention", "must be positive")
	}
	if strings.TrimSpace(m.Runtime.ProcessCompose.Executable) == "" {
		add("runtime.process_compose.executable", "is required")
	}
	if m.Terminal.Mode != "warp" && m.Terminal.Mode != "headless" {
		add("terminal.mode", "must be warp or headless")
	}

	names := make(map[string]int, len(m.Services))
	for i := range m.Services {
		s := &m.Services[i]
		prefix := fmt.Sprintf("services[%d]", i)
		if !serviceNamePattern.MatchString(s.Name) {
			add(prefix+".name", "must match [a-z][a-z0-9-]*")
		}
		if previous, exists := names[s.Name]; exists {
			add(prefix+".name", fmt.Sprintf("duplicates services[%d]", previous))
		} else {
			names[s.Name] = i
		}
		if s.Source != "native" && s.Source != "compose" && s.Source != "external" {
			add(prefix+".source", "must be native, compose, or external")
		}
		if s.Activation != "workspace" && s.Activation != "tab" {
			add(prefix+".activation", "must be workspace or tab")
		}
		if s.Source == "external" && s.Activation != "workspace" {
			add(prefix+".activation", "external services must use workspace activation")
		}
		validateWorkingDirectory(root, s.WorkingDirectory, prefix+".working_directory", add)
		blocks := 0
		if s.Run != nil {
			blocks++
		}
		if s.Compose != nil {
			blocks++
		}
		if s.External != nil {
			blocks++
		}
		if blocks != 1 {
			add(prefix, "must define exactly one of run, compose, or external")
		}
		switch s.Source {
		case "native":
			if s.Run == nil {
				add(prefix+".run", "is required for a native service")
			} else {
				validateArgv(s.Run.Argv, prefix+".run.argv", add)
			}
		case "compose":
			if s.Compose == nil {
				add(prefix+".compose", "is required for a compose service")
			} else {
				validateCompose(root, s, prefix, add)
			}
		case "external":
			if s.External == nil {
				add(prefix+".external", "is required for an external service")
			} else {
				validateExternal(s.External, prefix+".external", add)
			}
		}
		if s.Activation == "tab" {
			validateArgv(s.Terminal.TriggerArgv, prefix+".terminal.trigger_argv", add)
			if len(s.Terminal.TriggerArgv) > 0 && !triggerNamePattern.MatchString(s.Terminal.TriggerArgv[0]) {
				add(prefix+".terminal.trigger_argv[0]", "must be a simple executable name that can be wrapped by zsh")
			}
		}
		validateEnvironment(root, s, prefix, add)
		validateHealth(s.Health, prefix+".health", add)
		if s.Restart.Policy != "no" && s.Restart.Policy != "always" && s.Restart.Policy != "on-failure" {
			add(prefix+".restart.policy", "must be no, always, or on-failure")
		}
		if s.Restart.MaxRestarts < 0 {
			add(prefix+".restart.max_restarts", "must not be negative")
		}
		if s.Restart.Backoff.Duration < 0 {
			add(prefix+".restart.backoff", "must not be negative")
		}
		for portIndex, port := range s.Ports {
			if port < 1 || port > 65535 {
				add(fmt.Sprintf("%s.ports[%d]", prefix, portIndex), "must be between 1 and 65535")
			}
		}
	}

	allowedConditions := map[string]bool{"running": true, "healthy": true, "completed_successfully": true}
	for i, service := range m.Services {
		for dependency, condition := range service.DependsOn {
			path := fmt.Sprintf("services[%d].depends_on.%s", i, dependency)
			if _, exists := names[dependency]; !exists {
				add(path, "references an unknown service")
			}
			if !allowedConditions[condition] {
				add(path, "must be running, healthy, or completed_successfully")
			}
			if dependency == service.Name {
				add(path, "may not depend on itself")
			}
		}
	}
	if cycle := dependencyCycle(m.Services); len(cycle) > 0 {
		add("services", "dependency cycle: "+strings.Join(cycle, " -> "))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errs.New(errs.ExitUsage, "RG120", "invalid manifest:\n  - "+strings.Join(problems, "\n  - "))
	}
	return nil
}

func validateWorkingDirectory(root, directory, field string, add func(string, string)) {
	if filepath.IsAbs(directory) {
		add(field, "must be workspace-relative")
		return
	}
	clean := filepath.Clean(directory)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		add(field, "must remain within the workspace")
		return
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		add(field, "must name an existing directory")
		return
	}
	if !within(root, resolved) {
		add(field, "resolves outside the workspace")
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
		add(prefix+".compose.file", "must be workspace-relative")
	} else {
		filename := filepath.Join(root, service.WorkingDirectory, compose.File)
		resolved, err := filepath.EvalSymlinks(filename)
		if err != nil || !within(root, resolved) {
			add(prefix+".compose.file", "must name a file within the workspace")
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

func validateEnvironment(root string, service *Service, prefix string, add func(string, string)) {
	for key := range service.Environment.Values {
		if secretKeyPattern.MatchString(key) {
			add(prefix+".environment.values."+key, "secret-like keys must use an execution-time environment provider")
		}
	}
	for i, provider := range service.Environment.Providers {
		field := fmt.Sprintf("%s.environment.providers[%d]", prefix, i)
		switch provider.Type {
		case "dotenv":
			if provider.Path == "" {
				add(field+".path", "is required")
			} else if filepath.IsAbs(provider.Path) {
				add(field+".path", "must be workspace-relative")
			} else {
				candidate := filepath.Join(root, service.WorkingDirectory, provider.Path)
				if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
					if !within(root, resolved) {
						add(field+".path", "resolves outside the workspace")
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
				validateWorkingDirectory(root, filepath.Join(service.WorkingDirectory, provider.Directory), field+".directory", add)
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
