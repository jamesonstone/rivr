package onboarding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
	"gopkg.in/yaml.v3"
)

type Candidate struct {
	Name           string   `json:"name"`
	Source         string   `json:"source"`
	Directory      string   `json:"directory"`
	Argv           []string `json:"argv,omitempty"`
	ComposeFile    string   `json:"compose_file,omitempty"`
	ComposeService string   `json:"compose_service,omitempty"`
	Profiles       []string `json:"profiles,omitempty"`
	Confidence     string   `json:"confidence"`
	Evidence       string   `json:"evidence"`
}

type Draft struct {
	APIVersion       string `json:"api_version"`
	FlowVersion      int    `json:"flow_version"`
	DiscoveryHash    string `json:"discovery_hash"`
	Name             string `json:"name"`
	Terminal         string `json:"terminal"`
	Environment      string `json:"environment"`
	LinkDependencies bool   `json:"link_dependencies"`
	Selected         []bool `json:"selected"`
	Screen           int    `json:"screen"`
}

type Options struct {
	Root        string
	Destination string
	DraftPath   string
	Force       bool
	FromCompose string
	Input       io.Reader
	Output      io.Writer
	ErrorOutput io.Writer
}

type Result struct {
	Manifest *manifest.Manifest
	Content  []byte
	Resumed  bool
}

func Interactive(options Options) (Result, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return Result{}, err
	}
	candidates, discoveryHash, err := Discover(root, options.FromCompose)
	if err != nil {
		return Result{}, err
	}
	projectID, err := manifest.NewProjectID(manifest.Slug(filepath.Base(root)))
	if err != nil {
		return Result{}, err
	}
	initialModel := newModel(options, candidates, discoveryHash, projectID)
	program := tea.NewProgram(initialModel, tea.WithInput(options.Input), tea.WithOutput(options.Output), tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		return Result{}, errs.Wrap(errs.ExitFailure, "RG1301", "run onboarding", err)
	}
	completed, ok := finalModel.(model)
	if !ok || completed.cancelled {
		return Result{}, errs.New(errs.ExitInterrupted, "RG1302", "onboarding cancelled")
	}
	if !completed.done {
		return Result{}, errs.New(errs.ExitFailure, "RG1303", "onboarding ended before confirmation")
	}
	resultManifest := completed.buildManifest()
	content, err := yaml.Marshal(resultManifest)
	if err != nil {
		return Result{}, err
	}
	decoded, normalized, err := manifest.Decode(content, root)
	if err != nil {
		return Result{}, err
	}
	if err := WriteProjectFiles(options, normalized); err != nil {
		return Result{}, err
	}
	return Result{Manifest: decoded, Content: normalized, Resumed: completed.resumed}, nil
}

func NonInteractive(options Options, content []byte, name, terminal string) (Result, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return Result{}, err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		if strings.TrimSpace(name) == "" {
			return Result{}, errs.New(errs.ExitUsage, "RG1304", "non-interactive init requires --input or --name")
		}
		if terminal == "" {
			terminal = "headless"
		}
		projectID, err := manifest.NewProjectID(manifest.Slug(name))
		if err != nil {
			return Result{}, err
		}
		m := manifest.Manifest{APIVersion: manifest.APIVersion, Kind: manifest.Kind, Project: manifest.Project{Name: name, Slug: manifest.Slug(name), ID: projectID}, Terminal: manifest.Terminal{Mode: terminal}}
		if options.FromCompose != "" {
			candidates, _, err := Discover(root, options.FromCompose)
			if err != nil {
				return Result{}, err
			}
			for _, candidate := range candidates {
				if candidate.Source != "compose" {
					continue
				}
				m.Services = append(m.Services, candidateService(candidate))
			}
		}
		m.ApplyDefaults()
		content, err = yaml.Marshal(m)
		if err != nil {
			return Result{}, err
		}
	}
	decoded, normalized, err := manifest.Decode(content, root)
	if err != nil {
		return Result{}, err
	}
	if err := WriteProjectFiles(options, normalized); err != nil {
		return Result{}, err
	}
	return Result{Manifest: decoded, Content: normalized}, nil
}

func Discover(root, fromCompose string) ([]Candidate, string, error) {
	var candidates []Candidate
	composeFiles := []string{}
	if fromCompose != "" {
		composeFiles = append(composeFiles, fromCompose)
	} else {
		for _, pattern := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
			if _, err := os.Stat(filepath.Join(root, pattern)); err == nil {
				composeFiles = append(composeFiles, pattern)
			}
		}
	}
	for _, filename := range composeFiles {
		if filepath.IsAbs(filename) {
			return nil, "", errs.New(errs.ExitUsage, "RG1305", "Compose discovery path must be workspace-relative")
		}
		content, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			return nil, "", errs.Wrap(errs.ExitUsage, "RG1306", "read Compose discovery file", err)
		}
		var document struct {
			Services map[string]struct {
				Profiles []string `yaml:"profiles"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(content, &document); err != nil {
			return nil, "", errs.Wrap(errs.ExitUsage, "RG1307", "parse Compose discovery file", err)
		}
		names := make([]string, 0, len(document.Services))
		for name := range document.Services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			candidates = append(candidates, Candidate{Name: uniqueName(candidates, manifest.Slug(name)), Source: "compose", Directory: ".", ComposeFile: filepath.ToSlash(filename), ComposeService: name, Profiles: append([]string(nil), document.Services[name].Profiles...), Confidence: "exact", Evidence: "declared Compose service"})
		}
	}

	directories := []string{"."}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && entry.Name() != "node_modules" && entry.Name() != "vendor" {
			directories = append(directories, entry.Name())
		}
	}
	for _, directory := range directories {
		argv, confidence, evidence := inferCommand(filepath.Join(root, directory))
		if len(argv) == 0 {
			continue
		}
		name := manifest.Slug(filepath.Base(filepath.Join(root, directory)))
		if directory == "." {
			name = manifest.Slug(filepath.Base(root))
		}
		candidates = append(candidates, Candidate{Name: uniqueName(candidates, name), Source: "native", Directory: filepath.ToSlash(directory), Argv: argv, Confidence: confidence, Evidence: evidence})
	}
	hashContent, _ := json.Marshal(candidates)
	return candidates, state.Hash(hashContent), nil
}

func inferCommand(directory string) ([]string, string, string) {
	if content, err := os.ReadFile(filepath.Join(directory, "Makefile")); err == nil {
		if regexp.MustCompile(`(?m)^dev\s*:`).Match(content) {
			return []string{"make", "dev"}, "high", "Makefile dev target"
		}
	}
	if content, err := os.ReadFile(filepath.Join(directory, "package.json")); err == nil {
		var packageFile struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(content, &packageFile) == nil && packageFile.Scripts["dev"] != "" {
			return []string{"npm", "run", "dev"}, "high", "package.json dev script"
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(directory, "cmd"))
		for _, entry := range entries {
			if entry.IsDir() {
				return []string{"go", "run", "./cmd/" + entry.Name()}, "medium", "Go command directory"
			}
		}
	}
	return nil, "", ""
}

func WriteProjectFiles(options Options, content []byte) error {
	destination := options.Destination
	if destination == "" {
		destination = filepath.Join(options.Root, ".rungrid.yaml")
	}
	if _, err := os.Lstat(destination); err == nil && !options.Force {
		return errs.New(errs.ExitConflict, "RG1308", "manifest already exists; use --force after review")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := atomicWrite(destination, content, 0o644); err != nil {
		return err
	}
	gitignore := filepath.Join(options.Root, ".gitignore")
	existing, err := os.ReadFile(gitignore)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)
	for _, line := range []string{".rungrid.local.yaml", ".rungrid.draft.json"} {
		if !containsLine(text, line) {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += line + "\n"
		}
	}
	if err := atomicWrite(gitignore, []byte(text), 0o644); err != nil {
		return err
	}
	if options.DraftPath != "" {
		_ = os.Remove(options.DraftPath)
	}
	return nil
}

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
			if json.Unmarshal(content, &draft) == nil && draft.APIVersion == "rungrid/output/v1" && draft.FlowVersion == 2 && draft.DiscoveryHash == discoveryHash && len(draft.Selected) == len(selected) {
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
		switch key.String() {
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
	case 4:
		switch key.String() {
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

func (m model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	switch m.screen {
	case 0:
		return title.Render("Rungrid onboarding") + "\n\nProject name\n" + m.input.View() + "\n\n" + muted.Render("Enter continue • Ctrl-C cancel")
	case 1:
		return title.Render("Workspace path") + "\n\n" + m.options.Root + "\n\n" + muted.Render("Enter accept • b back")
	case 2:
		return title.Render("Terminal mode") + "\n\n  " + choice("warp", m.terminal) + "   " + choice("headless", m.terminal) + "\n\n" + muted.Render("←/→ choose • Enter continue • b back")
	case 3:
		var b strings.Builder
		b.WriteString(title.Render("Discovered services") + "\n\n")
		if len(m.candidates) == 0 {
			b.WriteString("No service commands were inferred. An empty manifest is valid.\n")
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
			fmt.Fprintf(&b, "%s[%s] %-18s %-8s %-6s %s\n", cursor, check, candidate.Name, candidate.Source, candidate.Confidence, candidate.Evidence)
		}
		b.WriteString("\n" + muted.Render("Space include/exclude • Enter continue • b back"))
		return b.String()
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

func (m model) buildManifest() *manifest.Manifest {
	result := &manifest.Manifest{APIVersion: manifest.APIVersion, Kind: manifest.Kind, Project: manifest.Project{Name: strings.TrimSpace(m.input.Value()), Slug: manifest.Slug(m.input.Value()), ID: m.projectID}, Terminal: manifest.Terminal{Mode: m.terminal}}
	for i, candidate := range m.candidates {
		if m.selected[i] {
			result.Services = append(result.Services, candidateService(candidate))
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

func candidateService(candidate Candidate) manifest.Service {
	service := manifest.Service{Name: candidate.Name, Source: candidate.Source, WorkingDirectory: candidate.Directory}
	if candidate.Source == "compose" {
		service.Activation = "workspace"
		service.Compose = &manifest.Compose{File: candidate.ComposeFile, Service: candidate.ComposeService, Profiles: append([]string(nil), candidate.Profiles...)}
	} else {
		service.Activation = "tab"
		service.Run = &manifest.Run{Argv: append([]string(nil), candidate.Argv...)}
		service.Terminal.TriggerArgv = append([]string(nil), candidate.Argv...)
	}
	return service
}

func (m model) saveDraft() {
	if m.options.DraftPath == "" {
		return
	}
	draft := Draft{APIVersion: "rungrid/output/v1", FlowVersion: 2, DiscoveryHash: m.discoveryHash, Name: m.input.Value(), Terminal: m.terminal, Environment: m.environment, LinkDependencies: m.linkDependencies, Selected: m.selected, Screen: m.screen}
	content, _ := json.MarshalIndent(draft, "", "  ")
	_ = atomicWrite(m.options.DraftPath, append(content, '\n'), 0o600)
}

func uniqueName(candidates []Candidate, base string) string {
	if base == "" {
		base = "service"
	}
	used := map[string]bool{}
	for _, c := range candidates {
		used[c.Name] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
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
func containsLine(content, line string) bool {
	for _, current := range strings.Split(content, "\n") {
		if strings.TrimSpace(current) == line {
			return true
		}
	}
	return false
}

func atomicWrite(filename string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errs.New(errs.ExitConflict, "RG1309", "refusing to replace a symlink: "+filename)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".rungrid-init-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, filename)
}
