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

func Discover(root, manifestDirectory, fromCompose string) ([]Candidate, string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", err
	}
	manifestDirectory, err = filepath.EvalSymlinks(manifestDirectory)
	if err != nil {
		return nil, "", err
	}
	if !discoveryPathWithin(root, manifestDirectory) {
		return nil, "", errs.New(errs.ExitUsage, "RG1310", "manifest directory must remain within the discovery workspace")
	}
	repositories := newRepositoryDiscovery(root, manifestDirectory)
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
		absolute, resolveErr := filepath.EvalSymlinks(filepath.Join(root, filename))
		if resolveErr != nil || !discoveryPathWithin(root, absolute) {
			return nil, "", errs.New(errs.ExitUsage, "RG1311", "Compose discovery file must remain within the workspace")
		}
		content, err := os.ReadFile(absolute)
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
		context := repositories.context(filepath.Dir(absolute))
		composeFile, _ := filepath.Rel(filepath.Join(context.root, context.directory), absolute)
		names := make([]string, 0, len(document.Services))
		for name := range document.Services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			candidates = append(candidates, Candidate{
				Name: uniqueName(candidates, manifest.Slug(name)), Source: "compose",
				Repository: context.name, RepositoryPath: context.path, Directory: context.directory,
				ComposeFile: filepath.ToSlash(composeFile), ComposeService: name,
				Profiles:   append([]string(nil), document.Services[name].Profiles...),
				Confidence: "exact", Evidence: "declared Compose service", AutoSelect: true,
			})
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
		absolute := filepath.Join(root, directory)
		argv, confidence, evidence := inferCommand(absolute)
		if len(argv) == 0 {
			continue
		}
		name := manifest.Slug(filepath.Base(absolute))
		if directory == "." {
			name = manifest.Slug(filepath.Base(root))
		}
		context := repositories.context(absolute)
		candidates = append(candidates, Candidate{
			Name: uniqueName(candidates, name), Source: "native",
			Repository: context.name, RepositoryPath: context.path, Directory: context.directory,
			Argv: argv, Confidence: confidence, Evidence: evidence,
			AutoSelect: context.root == repositories.manifestRoot && confidence == "high",
		})
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
	service := manifest.Service{Name: candidate.Name, Repository: candidate.Repository, Source: candidate.Source, WorkingDirectory: candidate.Directory}
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

func addCandidate(m *manifest.Manifest, candidate Candidate) {
	if candidate.Repository != "" && candidate.Repository != manifest.WorkspaceRepository {
		if m.Repositories == nil {
			m.Repositories = map[string]manifest.Repository{}
		}
		m.Repositories[candidate.Repository] = manifest.Repository{Path: candidate.RepositoryPath}
	}
	m.Services = append(m.Services, candidateService(candidate))
}

type repositoryDiscovery struct {
	root, manifestRoot string
	aliasesByPath      map[string]string
	pathsByAlias       map[string]string
}

type repositoryContext struct {
	name, path, root, directory string
}

func newRepositoryDiscovery(root, manifestDirectory string) *repositoryDiscovery {
	return &repositoryDiscovery{
		root: root, manifestRoot: nearestRepositoryRoot(root, manifestDirectory),
		aliasesByPath: map[string]string{}, pathsByAlias: map[string]string{},
	}
}

func (d *repositoryDiscovery) context(directory string) repositoryContext {
	repositoryRoot := nearestRepositoryRoot(d.root, directory)
	path, _ := filepath.Rel(d.root, repositoryRoot)
	workingDirectory, _ := filepath.Rel(repositoryRoot, directory)
	path = filepath.ToSlash(path)
	workingDirectory = filepath.ToSlash(workingDirectory)
	if path == "." {
		return repositoryContext{name: manifest.WorkspaceRepository, path: ".", root: repositoryRoot, directory: workingDirectory}
	}
	name := d.alias(path)
	return repositoryContext{name: name, path: path, root: repositoryRoot, directory: workingDirectory}
}

func (d *repositoryDiscovery) alias(path string) string {
	if name := d.aliasesByPath[path]; name != "" {
		return name
	}
	base := manifest.Slug(filepath.Base(path))
	if base == "" || base == manifest.WorkspaceRepository {
		base = "repository"
	}
	name := base
	for suffix := 2; d.pathsByAlias[name] != ""; suffix++ {
		name = fmt.Sprintf("%s-%d", base, suffix)
	}
	d.aliasesByPath[path] = name
	d.pathsByAlias[name] = path
	return name
}

func nearestRepositoryRoot(root, directory string) string {
	current := directory
	for discoveryPathWithin(root, current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		if current == root {
			break
		}
		current = filepath.Dir(current)
	}
	return root
}

func discoveryPathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
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
