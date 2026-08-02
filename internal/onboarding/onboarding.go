package onboarding

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
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
				if candidate.Source == "compose" {
					m.Services = append(m.Services, candidateService(candidate))
				}
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
