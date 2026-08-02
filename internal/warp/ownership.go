package warp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/state"
)

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
