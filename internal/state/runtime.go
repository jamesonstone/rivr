package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
)

func CurrentGeneration(layout Layout) (string, error) {
	content, err := os.ReadFile(filepath.Join(layout.ProjectDir, "current"))
	if err != nil {
		return "", errs.Wrap(errs.ExitConflict, "RG239", "read current generation", err)
	}
	value := strings.TrimSpace(string(content))
	if value == "" || strings.ContainsAny(value, `/\`) {
		return "", errs.New(errs.ExitConflict, "RG240", "invalid current generation identifier")
	}
	return value, nil
}

func RecordedRuntimeGeneration(layout Layout) (string, bool, error) {
	filename := filepath.Join(layout.ProjectDir, "runtime.json")
	info, statErr := os.Lstat(filename)
	if os.IsNotExist(statErr) {
		return "", false, nil
	}
	if statErr != nil {
		return "", false, errs.Wrap(errs.ExitConflict, "RG246", "inspect runtime generation guard", statErr)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", false, errs.New(errs.ExitConflict, "RG247", "runtime generation guard is not a private regular file")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", false, errs.Wrap(errs.ExitConflict, "RG248", "read runtime generation guard", err)
	}
	var record struct {
		APIVersion   string `json:"api_version"`
		ProjectID    string `json:"project_id"`
		GenerationID string `json:"generation_id"`
	}
	if json.Unmarshal(content, &record) != nil || record.APIVersion != ownershipAPI || record.ProjectID != layout.ProjectID || record.GenerationID == "" {
		return "", false, errs.New(errs.ExitConflict, "RG249", "runtime generation guard is invalid")
	}
	return record.GenerationID, true, nil
}

func Hash(parts ...[]byte) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write(part)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func RuntimeTimestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
