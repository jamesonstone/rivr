package onboarding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jamesonstone/rungrid/internal/manifest"
)

type model struct {
	options                       Options
	input                         textinput.Model
	candidates                    []Candidate
	selected                      []bool
	discoveryHash                 string
	projectID                     string
	terminal                      string
	environment                   string
	linkDependencies              bool
	screen, cursor, width, height int
	done, cancelled, resumed      bool
}

func newModel(options Options, candidates []Candidate, discoveryHash, projectID string) model {
	input := textinput.New()
	input.Placeholder = "Example Workspace"
	input.CharLimit = 80
	input.Width = 48
	input.SetValue(titleName(filepath.Base(options.Root)))
	input.Focus()
	selected := make([]bool, len(candidates))
	for i := range selected {
		selected[i] = candidates[i].Confidence == "exact" || candidates[i].Confidence == "high"
	}
	result := model{options: options, input: input, candidates: candidates, selected: selected, discoveryHash: discoveryHash, projectID: projectID, terminal: "warp", environment: "none", linkDependencies: true}
	if options.DraftPath != "" {
		if content, err := os.ReadFile(options.DraftPath); err == nil {
			var draft Draft
			if json.Unmarshal(content, &draft) == nil && draft.APIVersion == "rungrid/output/v1" && draft.FlowVersion == 3 && draft.DiscoveryHash == discoveryHash && draft.WorkspaceRoot == options.WorkspaceRoot && len(draft.Selected) == len(selected) {
				result.input.SetValue(draft.Name)
				result.terminal = draft.Terminal
				result.environment = draft.Environment
				result.linkDependencies = draft.LinkDependencies
				result.selected = draft.Selected
				result.screen = draft.Screen
				result.resumed = true
			}
		}
	}
	return result
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := message.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.input.Width = min(60, max(20, size.Width-10))
		return m, nil
	}
	key, ok := message.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		if m.screen == 0 {
			m.input, cmd = m.input.Update(message)
		}
		return m, cmd
	}
	if key.String() == "ctrl+c" || (key.String() == "q" && m.screen != 0) {
		m.cancelled = true
		return m, tea.Quit
	}
	switch m.screen {
	case 0:
		if key.String() == "enter" && strings.TrimSpace(m.input.Value()) != "" {
			if id, err := manifest.NewProjectID(manifest.Slug(m.input.Value())); err == nil {
				m.projectID = id
			}
			m.screen = 1
			m.saveDraft()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(message)
		return m, cmd
	case 1:
		switch key.String() {
		case "enter":
			m.screen = 2
			m.saveDraft()
		case "b", "backspace":
			m.screen = 0
			m.input.Focus()
		}
	case 2:
		switch key.String() {
		case "left", "right", " ":
			if m.terminal == "warp" {
				m.terminal = "headless"
			} else {
				m.terminal = "warp"
			}
		case "enter":
			m.screen = 3
			m.saveDraft()
		case "b", "backspace":
			m.screen = 1
		}
	case 3:
		m = m.updateServices(key.String())
	case 4:
		m = m.updateEnvironment(key.String())
	case 5:
		switch key.String() {
		case "left", "right", " ":
			m.linkDependencies = !m.linkDependencies
		case "enter":
			m.screen = 6
			m.saveDraft()
		case "b", "backspace":
			m.screen = 4
		}
	case 6:
		switch key.String() {
		case "y", "enter":
			m.done = true
			return m, tea.Quit
		case "b", "backspace":
			m.screen = 5
		}
	}
	return m, nil
}

func (m model) updateServices(key string) model {
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor+1 < len(m.candidates) {
			m.cursor++
		}
	case " ":
		if len(m.selected) > 0 {
			m.selected[m.cursor] = !m.selected[m.cursor]
		}
	case "enter":
		m.screen = 4
		m.saveDraft()
	case "b", "backspace":
		m.screen = 2
	}
	return m
}

func (m model) updateEnvironment(key string) model {
	switch key {
	case "left", "right", " ":
		switch m.environment {
		case "none":
			m.environment = "dotenv"
		case "dotenv":
			m.environment = "direnv"
		default:
			m.environment = "none"
		}
	case "enter":
		m.screen = 5
		m.saveDraft()
	case "b", "backspace":
		m.screen = 3
	}
	return m
}
