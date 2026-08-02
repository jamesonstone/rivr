package onboarding

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
	"gopkg.in/yaml.v3"
)

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

func uniqueName(candidates []Candidate, base string) string {
	if base == "" {
		base = "service"
	}
	used := map[string]bool{}
	for _, candidate := range candidates {
		used[candidate.Name] = true
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
