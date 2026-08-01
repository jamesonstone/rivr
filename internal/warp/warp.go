package warp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
)

const marker = "# rungrid-managed-tab-config"

type Template struct {
	Filename string
	TabName  string
	Service  string
	Content  []byte
}

type InstallRecord struct {
	APIVersion   string          `json:"api_version"`
	ProjectID    string          `json:"project_id"`
	GenerationID string          `json:"generation_id"`
	Artifacts    []InstalledFile `json:"artifacts"`
}

type InstalledFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Tab     string `json:"tab"`
	Service string `json:"service,omitempty"`
}

type renderedFile struct {
	template Template
	target   string
	content  []byte
}

func Templates(m *manifest.Manifest, generationID string) []Template {
	prefix := "rungrid_" + strings.ReplaceAll(m.Project.ID, "-", "_")
	result := []Template{
		newTemplate(prefix+"_00_overview", "Overview", "", m.Project.ID, generationID),
		newTemplate(prefix+"_01_versions", "Versions", "", m.Project.ID, generationID),
	}
	index := 2
	for _, service := range m.Services {
		if service.Activation != "tab" || service.Source == "external" {
			continue
		}
		tabName := fmt.Sprintf("%s_%02d_%s", prefix, index, strings.ReplaceAll(service.Name, "-", "_"))
		result = append(result, newTemplate(tabName, service.Terminal.Title, service.Name, m.Project.ID, generationID))
		index++
	}
	return result
}

func newTemplate(tabName, title, service, projectID, generationID string) Template {
	content := fmt.Sprintf(`%s
# project-id: %s
# generation-id: %s
name = %s
title = %s
color = "green"

[[panes]]
id = "main"
type = "terminal"
directory = "."
shell = "zsh"
commands = ["@@COMMAND@@"]
is_focused = true
`, marker, projectID, generationID, strconv.Quote(tabName), strconv.Quote(title))
	return Template{Filename: tabName + ".toml.tmpl", TabName: tabName, Service: service, Content: []byte(content)}
}

func Install(layout state.Layout, m *manifest.Manifest, generationID, rungridExecutable string) (InstallRecord, error) {
	if err := layout.Ensure(); err != nil {
		return InstallRecord{}, err
	}
	templates := Templates(m, generationID)
	destination, err := tabConfigDirectory()
	if err != nil {
		return InstallRecord{}, err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return InstallRecord{}, errs.Wrap(errs.ExitFailure, "RG901", "create Warp Tab Config directory", err)
	}
	previous, previousErr := readInstallRecord(layout)
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return InstallRecord{}, previousErr
	}
	if previousErr == nil {
		if err := verifyInstalled(previous); err != nil {
			return InstallRecord{}, err
		}
	}

	record := InstallRecord{APIVersion: "rungrid/output/v1", ProjectID: layout.ProjectID, GenerationID: generationID}
	expected := map[string]bool{}
	rendered := make([]renderedFile, 0, len(templates))
	for _, template := range templates {
		command := commandFor(template, layout, rungridExecutable, m.Project.ID, generationID)
		content := []byte(strings.ReplaceAll(string(template.Content), "@@COMMAND@@", escapeTOML(command)))
		target := filepath.Join(destination, strings.TrimSuffix(template.Filename, ".tmpl"))
		expected[target] = true
		if _, err := os.Lstat(target); err == nil {
			prior, found := findInstalled(previous, target)
			if !found {
				return InstallRecord{}, errs.New(errs.ExitConflict, "RG902", "refusing to replace an unrecorded Warp Tab Config: "+target)
			}
			if err := verifyInstalledFile(prior); err != nil {
				return InstallRecord{}, err
			}
		} else if !os.IsNotExist(err) {
			return InstallRecord{}, errs.Wrap(errs.ExitConflict, "RG903", "inspect Warp Tab Config", err)
		}
		rendered = append(rendered, renderedFile{template: template, target: target, content: content})
	}
	if previousErr == nil {
		for _, old := range previous.Artifacts {
			if expected[old.Path] {
				continue
			}
			if err := verifyInstalledFile(old); err != nil {
				return InstallRecord{}, err
			}
		}
	}
	for _, item := range rendered {
		if err := atomicWrite(item.target, item.content, 0o600); err != nil {
			return InstallRecord{}, err
		}
		record.Artifacts = append(record.Artifacts, InstalledFile{Path: item.target, SHA256: hash(item.content), Tab: item.template.TabName, Service: item.template.Service})
	}
	if previousErr == nil {
		for _, old := range previous.Artifacts {
			if expected[old.Path] {
				continue
			}
			if err := os.Remove(old.Path); err != nil && !os.IsNotExist(err) {
				return InstallRecord{}, errs.Wrap(errs.ExitConflict, "RG904", "remove stale owned Warp Tab Config", err)
			}
		}
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return InstallRecord{}, errs.Wrap(errs.ExitFailure, "RG905", "encode Warp install ownership", err)
	}
	if err := state.WriteFileAtomic(layout.ProjectDir, "terminal-install.json", append(content, '\n'), 0o600); err != nil {
		return InstallRecord{}, err
	}
	return record, nil
}

func Open(ctx context.Context, record InstallRecord, service string) error {
	if err := exec.CommandContext(ctx, "/usr/bin/open", "-Ra", "Warp").Run(); err != nil {
		return errs.Wrap(errs.ExitDependency, "RG906", "Warp is not installed or discoverable", err)
	}
	artifacts, err := selectArtifacts(record, service)
	if err != nil {
		return err
	}
	for index, artifact := range artifacts {
		if err := verifyInstalledFile(artifact); err != nil {
			return err
		}
		uri := uriFor(artifact.Tab, index == 0 && service == "")
		if err := exec.CommandContext(ctx, "/usr/bin/open", uri).Run(); err != nil {
			return errs.Wrap(errs.ExitFailure, "RG908", "open Warp Tab Config", err)
		}
		if index == 0 && service == "" {
			time.Sleep(time.Second)
		} else if index+1 < len(artifacts) {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}

func URIs(record InstallRecord, service string) ([]string, error) {
	artifacts, err := selectArtifacts(record, service)
	if err != nil {
		return nil, err
	}
	result := make([]string, len(artifacts))
	for index, artifact := range artifacts {
		result[index] = uriFor(artifact.Tab, index == 0 && service == "")
	}
	return result, nil
}

func selectArtifacts(record InstallRecord, service string) ([]InstalledFile, error) {
	var artifacts []InstalledFile
	if service == "" {
		artifacts = record.Artifacts
	} else {
		for _, artifact := range record.Artifacts {
			if artifact.Service == service {
				artifacts = append(artifacts, artifact)
				break
			}
		}
		if len(artifacts) == 0 {
			return nil, errs.New(errs.ExitUsage, "RG907", "service has no Warp tab: "+service)
		}
	}
	return artifacts, nil
}

func uriFor(tab string, newWindow bool) string {
	uri := "warp://tab_config/" + tab
	if newWindow {
		uri += "?new_window=true"
	}
	return uri
}

func Uninstall(layout state.Layout) error {
	record, err := readInstallRecord(layout)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, artifact := range record.Artifacts {
		if err := verifyInstalledFile(artifact); err != nil {
			return err
		}
	}
	for _, artifact := range record.Artifacts {
		if err := os.Remove(artifact.Path); err != nil && !os.IsNotExist(err) {
			return errs.Wrap(errs.ExitPartial, "RG909", "remove owned Warp Tab Config", err)
		}
	}
	if err := os.Remove(filepath.Join(layout.ProjectDir, "terminal-install.json")); err != nil && !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitPartial, "RG910", "remove Warp install ownership", err)
	}
	return nil
}

func ReadInstallRecord(layout state.Layout) (InstallRecord, error) { return readInstallRecord(layout) }

func commandFor(template Template, layout state.Layout, executable, projectID, generationID string) string {
	global := shellQuote(executable) + " --state-dir " + shellQuote(layout.StateRoot) + " --project " + shellQuote(projectID)
	if strings.HasSuffix(template.TabName, "_00_overview") {
		return "exec " + global + " attach --read-only"
	}
	if strings.HasSuffix(template.TabName, "_01_versions") {
		return "exec " + global + " versions --watch"
	}
	return "exec " + global + " internal service-shell --generation " + shellQuote(generationID) + " --service " + shellQuote(template.Service)
}

func readInstallRecord(layout state.Layout) (InstallRecord, error) {
	content, err := os.ReadFile(filepath.Join(layout.ProjectDir, "terminal-install.json"))
	if err != nil {
		return InstallRecord{}, err
	}
	var record InstallRecord
	if err := json.Unmarshal(content, &record); err != nil {
		return InstallRecord{}, errs.Wrap(errs.ExitConflict, "RG911", "decode Warp install ownership", err)
	}
	if record.APIVersion != "rungrid/output/v1" || record.ProjectID != layout.ProjectID {
		return InstallRecord{}, errs.New(errs.ExitConflict, "RG912", "Warp install ownership belongs to another project")
	}
	return record, nil
}

func verifyInstalled(record InstallRecord) error {
	for _, artifact := range record.Artifacts {
		if err := verifyInstalledFile(artifact); err != nil {
			return err
		}
	}
	return nil
}

func verifyInstalledFile(artifact InstalledFile) error {
	info, err := os.Lstat(artifact.Path)
	if err != nil {
		return errs.Wrap(errs.ExitConflict, "RG913", "inspect owned Warp Tab Config", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errs.New(errs.ExitConflict, "RG914", "owned Warp Tab Config is no longer a regular file: "+artifact.Path)
	}
	content, err := os.ReadFile(artifact.Path)
	if err != nil {
		return errs.Wrap(errs.ExitConflict, "RG915", "read owned Warp Tab Config", err)
	}
	if !strings.HasPrefix(string(content), marker+"\n") || hash(content) != artifact.SHA256 {
		return errs.New(errs.ExitConflict, "RG916", "owned Warp Tab Config was modified: "+artifact.Path)
	}
	return nil
}

func findInstalled(record InstallRecord, target string) (InstalledFile, bool) {
	for _, artifact := range record.Artifacts {
		if artifact.Path == target {
			return artifact, true
		}
	}
	return InstalledFile{}, false
}

func tabConfigDirectory() (string, error) {
	if configured := os.Getenv("WARP_TAB_CONFIG_DIR"); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", errs.New(errs.ExitUsage, "RG917", "WARP_TAB_CONFIG_DIR must be absolute")
		}
		return filepath.Clean(configured), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errs.Wrap(errs.ExitFailure, "RG918", "resolve home for Warp Tab Configs", err)
	}
	return filepath.Join(home, ".warp", "tab_configs"), nil
}

func atomicWrite(filename string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errs.New(errs.ExitConflict, "RG919", "refusing to replace a Warp Tab Config symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitConflict, "RG920", "inspect Warp Tab Config target", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".rungrid-tab-")
	if err != nil {
		return errs.Wrap(errs.ExitFailure, "RG921", "create Warp Tab Config temporary file", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
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
	return os.Rename(temporaryName, filename)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func escapeTOML(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

func hash(content []byte) string {
	value := sha256.Sum256(content)
	return hex.EncodeToString(value[:])
}

func SortedArtifacts(record InstallRecord) []InstalledFile {
	result := append([]InstalledFile(nil), record.Artifacts...)
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}
