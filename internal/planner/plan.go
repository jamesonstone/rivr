package planner

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
)

type Plan struct {
	APIVersion     string        `json:"api_version"`
	ProjectID      string        `json:"project_id"`
	GenerationID   string        `json:"generation_id"`
	ManifestSHA256 string        `json:"manifest_sha256"`
	Services       []ServicePlan `json:"services"`
	Artifacts      []string      `json:"artifacts"`
	Executables    []string      `json:"executables"`
	TerminalMode   string        `json:"terminal_mode"`
	OpenTerminal   bool          `json:"open_terminal"`
}

type ServicePlan struct {
	Name         string            `json:"name"`
	Source       string            `json:"source"`
	Activation   string            `json:"activation"`
	Process      bool              `json:"process_compose_process"`
	Disabled     bool              `json:"disabled"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
	Actions      []string          `json:"actions"`
}

func Build(loaded *manifest.Loaded, generatorVersion string) Plan {
	manifestHash := state.Hash(loaded.MergedYAML)
	generationHash := state.Hash(loaded.MergedYAML, []byte(generatorVersion))
	plan := Plan{
		APIVersion:     "rungrid/output/v1",
		ProjectID:      loaded.Manifest.Project.ID,
		GenerationID:   generationHash[:20],
		ManifestSHA256: manifestHash,
		Artifacts: []string{
			"manifest.yaml",
			"plan.json",
			"process-compose.yaml",
		},
		TerminalMode: loaded.Manifest.Terminal.Mode,
		OpenTerminal: loaded.Manifest.Terminal.Open != nil && *loaded.Manifest.Terminal.Open,
	}
	if loaded.Manifest.Terminal.Mode == "warp" {
		plan.Artifacts = append(plan.Artifacts,
			"terminal/warp/00_overview.toml.tmpl",
			"terminal/warp/01_versions.toml.tmpl",
		)
	}
	executables := map[string]bool{loaded.Manifest.Runtime.ProcessCompose.Executable: true}
	if loaded.Manifest.Terminal.Mode == "warp" {
		executables["zsh"] = true
	}
	tabIndex := 2
	for _, service := range loaded.Manifest.Services {
		item := ServicePlan{
			Name:         service.Name,
			Source:       service.Source,
			Activation:   service.Activation,
			Process:      service.Source != "external",
			Disabled:     service.Activation == "tab",
			Dependencies: service.DependsOn,
		}
		switch service.Source {
		case "native":
			item.Actions = []string{"resolve environment", "start supervised native process"}
			if service.Run != nil && len(service.Run.Argv) > 0 {
				executables[service.Run.Argv[0]] = true
			}
		case "compose":
			item.Actions = []string{"resolve environment", "start exact Compose service", "record exact Compose shutdown"}
			if service.Compose != nil && len(service.Compose.UpArgv) > 0 {
				executables[service.Compose.UpArgv[0]] = true
			}
		case "external":
			item.Actions = []string{"observe external readiness"}
		}
		if service.Activation == "tab" {
			item.Actions = append(item.Actions, "wait for exclusive service session")
			plan.Artifacts = append(plan.Artifacts,
				fmt.Sprintf("terminal/warp/%02d_%s.toml.tmpl", tabIndex, service.Name),
				"wrappers/"+service.Name,
			)
			tabIndex++
		} else if service.Source != "external" {
			plan.Artifacts = append(plan.Artifacts, "wrappers/"+service.Name)
		}
		for _, provider := range service.Environment.Providers {
			switch provider.Type {
			case "command":
				if len(provider.Argv) > 0 {
					executables[provider.Argv[0]] = true
				}
			case "direnv":
				executables["direnv"] = true
			}
		}
		plan.Services = append(plan.Services, item)
	}
	for executable := range executables {
		plan.Executables = append(plan.Executables, executable)
	}
	sort.Strings(plan.Executables)
	return plan
}

func (p Plan) JSON() ([]byte, error) {
	content, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func (p Plan) WriteHuman(w io.Writer) {
	_, _ = fmt.Fprintf(w, "Project: %s\nGeneration: %s\nTerminal: %s\n\n", p.ProjectID, p.GenerationID, p.TerminalMode)
	_, _ = fmt.Fprintln(w, "Services:")
	for _, service := range p.Services {
		stateText := "enabled"
		if service.Disabled {
			stateText = "disabled until session ownership"
		} else if !service.Process {
			stateText = "observed only"
		}
		_, _ = fmt.Fprintf(w, "  %-20s %-9s %-9s %s\n", service.Name, service.Source, service.Activation, stateText)
	}
	_, _ = fmt.Fprintln(w, "\nArtifacts:")
	for _, artifact := range p.Artifacts {
		_, _ = fmt.Fprintf(w, "  %s\n", artifact)
	}
	_, _ = fmt.Fprintf(w, "\nRequired executables: %s\n", strings.Join(p.Executables, ", "))
}
