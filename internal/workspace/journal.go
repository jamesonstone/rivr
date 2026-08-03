package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/state"
)

const (
	JournalAPI      = "rungrid/output/v1"
	StateInactive   = "inactive"
	StateStarting   = "starting"
	StateActive     = "active"
	StateStopping   = "stopping"
	StateCleanup    = "cleanup-required"
	journalFilename = "lifecycle.json"
)

type Journal struct {
	APIVersion       string           `json:"api_version"`
	ProjectID        string           `json:"project_id"`
	GenerationID     string           `json:"generation_id"`
	ManifestSHA256   string           `json:"manifest_sha256"`
	LifecycleSHA256  string           `json:"lifecycle_sha256"`
	WorkspaceRoot    string           `json:"workspace_root"`
	State            string           `json:"state"`
	CompletedBefore  []string         `json:"completed_before_up"`
	TeardownRequired bool             `json:"teardown_required"`
	Runtime          *RuntimeIdentity `json:"runtime,omitempty"`
	StartedAt        string           `json:"started_at"`
	UpdatedAt        string           `json:"updated_at"`
	Outcomes         []CommandOutcome `json:"outcomes,omitempty"`
	CleanupFailure   string           `json:"cleanup_failure,omitempty"`
}

type RuntimeIdentity struct {
	PID             int    `json:"pid"`
	ProcessIdentity string `json:"process_identity"`
	Socket          string `json:"socket"`
	SocketDevice    uint64 `json:"socket_device"`
	SocketInode     uint64 `json:"socket_inode"`
}

type CommandOutcome struct {
	Phase      string `json:"phase"`
	Name       string `json:"name"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	ExitCode   int    `json:"exit_code,omitempty"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	Log        string `json:"log,omitempty"`
}

func NewJournal(projectID, generationID, manifestHash, lifecycleHash, root string, teardownRequired bool) Journal {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Journal{
		APIVersion: JournalAPI, ProjectID: projectID, GenerationID: generationID,
		ManifestSHA256: manifestHash, LifecycleSHA256: lifecycleHash, WorkspaceRoot: root,
		State: StateStarting, TeardownRequired: teardownRequired, StartedAt: now, UpdatedAt: now,
	}
}

func ReadJournal(layout state.Layout) (Journal, error) {
	filename := filepath.Join(layout.ProjectDir, journalFilename)
	info, err := os.Lstat(filename)
	if err != nil {
		return Journal{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return Journal{}, errs.New(errs.ExitConflict, "RG1501", "lifecycle journal is not a private regular file")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return Journal{}, errs.Wrap(errs.ExitConflict, "RG1502", "read lifecycle journal", err)
	}
	var journal Journal
	if err := json.Unmarshal(content, &journal); err != nil {
		return Journal{}, errs.Wrap(errs.ExitConflict, "RG1503", "decode lifecycle journal", err)
	}
	if err := validateJournal(layout, journal); err != nil {
		return Journal{}, err
	}
	return journal, nil
}

func WriteJournal(layout state.Layout, journal Journal) error {
	if err := validateJournal(layout, journal); err != nil {
		return err
	}
	journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	content, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return errs.Wrap(errs.ExitFailure, "RG1504", "encode lifecycle journal", err)
	}
	return state.WriteFileAtomic(layout.ProjectDir, journalFilename, append(content, '\n'), 0o600)
}

func ReadJournalIfPresent(layout state.Layout) (Journal, bool, error) {
	journal, err := ReadJournal(layout)
	if errors.Is(err, os.ErrNotExist) {
		return Journal{}, false, nil
	}
	return journal, err == nil, err
}

func (j *Journal) Record(outcome CommandOutcome) {
	j.Outcomes = append(j.Outcomes, outcome)
	if outcome.Phase == "before_up" && outcome.Status == "succeeded" && !slices.Contains(j.CompletedBefore, outcome.Name) {
		j.CompletedBefore = append(j.CompletedBefore, outcome.Name)
	}
}

func validateJournal(layout state.Layout, journal Journal) error {
	validStates := map[string]bool{
		StateInactive: true, StateStarting: true, StateActive: true,
		StateStopping: true, StateCleanup: true,
	}
	if journal.APIVersion != JournalAPI || journal.ProjectID != layout.ProjectID || journal.GenerationID == "" ||
		journal.ManifestSHA256 == "" || journal.LifecycleSHA256 == "" || journal.WorkspaceRoot == "" || !validStates[journal.State] {
		return errs.New(errs.ExitConflict, "RG1505", "lifecycle journal identity or state is invalid")
	}
	if !filepath.IsAbs(journal.WorkspaceRoot) {
		return errs.New(errs.ExitConflict, "RG1506", "lifecycle journal workspace root must be absolute runtime state")
	}
	return nil
}
