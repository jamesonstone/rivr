package onboarding

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"gopkg.in/yaml.v3"
)

func (m model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	switch m.screen {
	case 0:
		return title.Render("Rungrid onboarding") + "\n\nProject name\n" + m.input.View() + "\n\n" + muted.Render("Enter continue • Ctrl-C cancel")
	case 1:
		return title.Render("Workspace path") + "\n\nManifest directory: " + m.options.Root + "\nWorkspace root: " + m.options.WorkspaceRoot + "\n\n" + muted.Render("Enter accept • b back")
	case 2:
		return title.Render("Terminal mode") + "\n\n  " + choice("warp", m.terminal) + "   " + choice("headless", m.terminal) + "\n\n" + muted.Render("←/→ choose • Enter continue • b back")
	case 3:
		return m.servicesView(title, muted)
	case 4:
		return title.Render("Environment provider") + "\n\n  " + choice("none", m.environment) + "   " + choice("dotenv", m.environment) + "   " + choice("direnv", m.environment) + "\n\n" + muted.Render("←/→ choose • dotenv is optional .env • Enter continue • b back")
	case 5:
		selection := "no"
		if m.linkDependencies {
			selection = "yes"
		}
		return title.Render("Dependencies") + "\n\nMake selected native services depend on selected Compose services?  [" + selection + "]\n\n" + muted.Render("←/→ toggle • Enter review • b back")
	case 6:
		content, _ := yaml.Marshal(m.buildManifest())
		return title.Render("Final review") + "\n\n" + string(content) + "\n" + muted.Render("y/Enter write atomically • b back • q cancel")
	default:
		return ""
	}
}

func (m model) servicesView(title, muted lipgloss.Style) string {
	var builder strings.Builder
	builder.WriteString(title.Render("Discovered services") + "\n\n")
	if len(m.candidates) == 0 {
		builder.WriteString("No service commands were inferred. An empty manifest is valid.\n")
	}
	for i, candidate := range m.candidates {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		check := " "
		if m.selected[i] {
			check = "x"
		}
		fmt.Fprintf(&builder, "%s[%s] %-18s %-8s %-6s %s\n", cursor, check, candidate.Name, candidate.Source, candidate.Confidence, candidate.Evidence)
	}
	builder.WriteString("\n" + muted.Render("Space include/exclude • Enter continue • b back"))
	return builder.String()
}

func (m model) buildManifest() *manifest.Manifest {
	result := &manifest.Manifest{APIVersion: manifest.APIVersion, Kind: manifest.Kind, Project: manifest.Project{Name: strings.TrimSpace(m.input.Value()), Slug: manifest.Slug(m.input.Value()), ID: m.projectID}, Workspace: manifest.Workspace{Root: m.options.WorkspaceRoot}, Terminal: manifest.Terminal{Mode: m.terminal}}
	for i, candidate := range m.candidates {
		if m.selected[i] {
			addCandidate(result, candidate)
		}
	}
	var composeServices []string
	for i := range result.Services {
		if result.Services[i].Source == "compose" {
			composeServices = append(composeServices, result.Services[i].Name)
		}
	}
	for i := range result.Services {
		service := &result.Services[i]
		if service.Source != "native" {
			continue
		}
		switch m.environment {
		case "dotenv":
			service.Environment.Providers = []manifest.EnvironmentProvider{{Type: "dotenv", Path: ".env", Optional: true}}
		case "direnv":
			service.Environment.Providers = []manifest.EnvironmentProvider{{Type: "direnv", Directory: "."}}
		}
		if m.linkDependencies && len(composeServices) > 0 {
			service.DependsOn = map[string]string{}
			for _, dependency := range composeServices {
				service.DependsOn[dependency] = "running"
			}
		}
	}
	result.ApplyDefaults()
	return result
}

func (m model) saveDraft() {
	if m.options.DraftPath == "" {
		return
	}
	draft := Draft{APIVersion: "rungrid/output/v1", FlowVersion: 3, DiscoveryHash: m.discoveryHash, Name: m.input.Value(), Terminal: m.terminal, Environment: m.environment, WorkspaceRoot: m.options.WorkspaceRoot, LinkDependencies: m.linkDependencies, Selected: m.selected, Screen: m.screen}
	content, _ := json.MarshalIndent(draft, "", "  ")
	_ = atomicWrite(m.options.DraftPath, append(content, '\n'), 0o600)
}

func titleName(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' })
	for i := range parts {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, " ")
}

func choice(value, selected string) string {
	if value == selected {
		return "[" + value + "]"
	}
	return " " + value + " "
}
