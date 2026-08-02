package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
	"gopkg.in/yaml.v3"
)

type Loaded struct {
	Manifest    Manifest
	Root        string
	ConfigPath  string
	LocalPath   string
	SourceFiles []string
	MergedYAML  []byte
}

func LoadGenerated(filename, root string) (*Manifest, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, errs.Wrap(errs.ExitConflict, "RG117", "read generated manifest", err)
	}
	var result Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&result); err != nil {
		return nil, errs.Wrap(errs.ExitConflict, "RG118", "decode generated manifest", err)
	}
	result.ApplyDefaults()
	if err := Validate(&result, root); err != nil {
		return nil, err
	}
	return &result, nil
}

func Decode(content []byte, root string) (*Manifest, []byte, error) {
	var result Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&result); err != nil {
		return nil, nil, errs.Wrap(errs.ExitUsage, "RG119", "decode manifest", err)
	}
	result.ApplyDefaults()
	if result.Project.ID == "" && result.Project.Slug != "" {
		projectID, err := NewProjectID(result.Project.Slug)
		if err != nil {
			return nil, nil, err
		}
		result.Project.ID = projectID
	}
	if err := Validate(&result, root); err != nil {
		return nil, nil, err
	}
	normalized, err := yaml.Marshal(result)
	if err != nil {
		return nil, nil, errs.Wrap(errs.ExitFailure, "RG121", "encode normalized manifest", err)
	}
	return &result, normalized, nil
}

func Load(configPath, localPath string) (*Loaded, error) {
	if configPath == "" {
		configPath = ".rungrid.yaml"
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG101", "resolve manifest path", err)
	}
	absConfig = filepath.Clean(absConfig)
	root := filepath.Dir(absConfig)
	resolvedRoot, err := resolveExisting(root)
	if err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG102", "resolve workspace root", err)
	}
	files := make([]string, 0, 4)
	merged, err := loadMap(absConfig, resolvedRoot, map[string]bool{}, &files)
	if err != nil {
		return nil, err
	}

	absLocal := ""
	if localPath == "" {
		absLocal = filepath.Join(root, ".rungrid.local.yaml")
	} else {
		absLocal, err = filepath.Abs(localPath)
		if err != nil {
			return nil, errs.Wrap(errs.ExitUsage, "RG103", "resolve local overlay path", err)
		}
	}
	if _, statErr := os.Stat(absLocal); statErr == nil {
		localMap, loadErr := loadMap(absLocal, resolvedRoot, map[string]bool{}, &files)
		if loadErr != nil {
			return nil, loadErr
		}
		merged = mergeMap(merged, localMap, "")
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return nil, errs.Wrap(errs.ExitUsage, "RG104", "inspect local overlay", statErr)
	} else if localPath != "" {
		return nil, errs.Wrap(errs.ExitUsage, "RG105", "read requested local overlay", statErr)
	}

	encoded, err := yaml.Marshal(merged)
	if err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG106", "encode merged manifest", err)
	}
	var result Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(encoded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&result); err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG107", "decode merged manifest", err)
	}
	result.ApplyDefaults()
	if err := Validate(&result, resolvedRoot); err != nil {
		return nil, err
	}
	canonical, err := yaml.Marshal(result)
	if err != nil {
		return nil, errs.Wrap(errs.ExitFailure, "RG108", "encode normalized manifest", err)
	}
	return &Loaded{
		Manifest:    result,
		Root:        resolvedRoot,
		ConfigPath:  absConfig,
		LocalPath:   absLocal,
		SourceFiles: files,
		MergedYAML:  canonical,
	}, nil
}

func loadMap(filename, root string, stack map[string]bool, files *[]string) (map[string]any, error) {
	resolved, err := resolveExisting(filename)
	if err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG109", "read manifest", err)
	}
	if !within(root, resolved) {
		return nil, errs.New(errs.ExitUsage, "RG110", fmt.Sprintf("manifest import escapes workspace: %s", filename))
	}
	if stack[resolved] {
		return nil, errs.New(errs.ExitUsage, "RG111", fmt.Sprintf("manifest import cycle at %s", filename))
	}
	stack[resolved] = true
	defer delete(stack, resolved)

	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG112", "read manifest", err)
	}
	var current map[string]any
	if err := yaml.Unmarshal(content, &current); err != nil {
		return nil, errs.Wrap(errs.ExitUsage, "RG113", "parse manifest YAML", err)
	}
	if current == nil {
		return nil, errs.New(errs.ExitUsage, "RG114", "manifest must be a YAML mapping")
	}
	*files = append(*files, resolved)

	result := map[string]any{}
	if rawImports, ok := current["imports"]; ok {
		imports, ok := rawImports.([]any)
		if !ok {
			return nil, errs.New(errs.ExitUsage, "RG115", "imports must be a sequence of paths")
		}
		for _, value := range imports {
			name, ok := value.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return nil, errs.New(errs.ExitUsage, "RG116", "each import must be a non-empty path")
			}
			imported, importErr := loadMap(filepath.Join(filepath.Dir(resolved), name), root, stack, files)
			if importErr != nil {
				return nil, importErr
			}
			result = mergeMap(result, imported, "")
		}
	}
	return mergeMap(result, current, ""), nil
}

func mergeMap(base, overlay map[string]any, parent string) map[string]any {
	result := cloneMap(base)
	for key, overlayValue := range overlay {
		baseValue, exists := result[key]
		if !exists {
			result[key] = cloneValue(overlayValue)
			continue
		}
		baseMap, baseOK := baseValue.(map[string]any)
		overlayMap, overlayOK := overlayValue.(map[string]any)
		if baseOK && overlayOK {
			result[key] = mergeMap(baseMap, overlayMap, key)
			continue
		}
		if key == "services" && parent == "" {
			baseList, baseListOK := baseValue.([]any)
			overlayList, overlayListOK := overlayValue.([]any)
			if baseListOK && overlayListOK {
				result[key] = mergeServices(baseList, overlayList)
				continue
			}
		}
		result[key] = cloneValue(overlayValue)
	}
	return result
}

func mergeServices(base, overlay []any) []any {
	result := make([]any, len(base))
	index := map[string]int{}
	for i, value := range base {
		result[i] = cloneValue(value)
		if service, ok := value.(map[string]any); ok {
			if name, ok := service["name"].(string); ok {
				index[name] = i
			}
		}
	}
	for _, value := range overlay {
		service, isService := value.(map[string]any)
		name, hasName := service["name"].(string)
		if isService && hasName {
			if position, exists := index[name]; exists {
				if existing, ok := result[position].(map[string]any); ok {
					result[position] = mergeMap(existing, service, "services")
					continue
				}
			}
			index[name] = len(result)
		}
		result = append(result, cloneValue(value))
	}
	return result
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = cloneValue(item)
	}
	return result
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = cloneValue(item)
		}
		return result
	default:
		return value
	}
}

func resolveExisting(filename string) (string, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(abs))
}

func within(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
