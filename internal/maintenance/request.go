package maintenance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/state"
)

const (
	SyncProcessName  = "rungrid-maintenance-sync"
	PruneProcessName = "rungrid-maintenance-worktrees-prune"
)

type Request struct {
	APIVersion   string   `json:"api_version"`
	ProjectID    string   `json:"project_id"`
	GenerationID string   `json:"generation_id"`
	RequestID    string   `json:"request_id"`
	Operation    string   `json:"operation"`
	Repositories []string `json:"repositories,omitempty"`
	Authorized   bool     `json:"authorized"`
	CreatedAt    string   `json:"created_at"`
	ExpiresAt    string   `json:"expires_at"`
}

type JobResult struct {
	APIVersion string          `json:"api_version"`
	ProjectID  string          `json:"project_id"`
	RequestID  string          `json:"request_id"`
	Operation  string          `json:"operation"`
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data"`
	ErrorCode  int             `json:"error_code,omitempty"`
	Diagnostic string          `json:"diagnostic,omitempty"`
	Error      string          `json:"error,omitempty"`
	FinishedAt string          `json:"finished_at"`
}

func NewRequest(layout state.Layout, generationID, operation string, repositories []string) (Request, error) {
	if !validJobIdentity(generationID, operation) {
		return Request{}, errs.New(errs.ExitConflict, "RG1626", "maintenance generation or operation is invalid")
	}
	requestID, err := maintenanceRequestID()
	if err != nil {
		return Request{}, err
	}
	now := time.Now().UTC()
	return Request{
		APIVersion: "rungrid/output/v1", ProjectID: layout.ProjectID,
		GenerationID: generationID, RequestID: requestID, Operation: operation,
		Repositories: append([]string(nil), repositories...), Authorized: true,
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(2 * time.Minute).Format(time.RFC3339Nano),
	}, nil
}

func WriteRequest(layout state.Layout, request Request) error {
	if !validRequest(request) || request.ProjectID != layout.ProjectID {
		return errs.New(errs.ExitConflict, "RG1622", "maintenance request identity or authorization is invalid")
	}
	if err := layout.Ensure(); err != nil {
		return err
	}
	path := pendingRequestPath(layout, request.GenerationID, request.Operation)
	if existing, err := os.ReadFile(path); err == nil {
		var current Request
		if json.Unmarshal(existing, &current) == nil && !requestIsExpired(current) {
			return errs.New(errs.ExitConflict, "RG1620", "a repository maintenance request is already pending")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	content, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Dir(path), filepath.Base(path), append(content, '\n'), 0o600)
}

func ClaimRequest(layout state.Layout, generationID, operation string) (Request, string, error) {
	if !validJobIdentity(generationID, operation) {
		return Request{}, "", errs.New(errs.ExitConflict, "RG1622", "maintenance request identity or authorization is invalid")
	}
	pending := pendingRequestPath(layout, generationID, operation)
	content, err := readPrivateFile(pending)
	if err != nil {
		return Request{}, "", errs.Wrap(errs.ExitConflict, "RG1621", "no authorized maintenance request is pending", err)
	}
	var request Request
	if json.Unmarshal(content, &request) != nil || !validRequest(request) ||
		request.ProjectID != layout.ProjectID || request.GenerationID != generationID ||
		request.Operation != operation || requestIsExpired(request) {
		return Request{}, "", errs.New(errs.ExitConflict, "RG1622", "maintenance request identity or authorization is invalid")
	}
	claimed := filepath.Join(filepath.Dir(pending), fmt.Sprintf("%s-%s-%s.claimed.json", generationID, operation, request.RequestID))
	if err := os.Rename(pending, claimed); err != nil {
		return Request{}, "", errs.Wrap(errs.ExitConflict, "RG1623", "claim maintenance request", err)
	}
	return request, claimed, nil
}

func WriteJobResult(layout state.Layout, request Request, data any, runErr error) error {
	if !validRequest(request) || request.ProjectID != layout.ProjectID {
		return errs.New(errs.ExitConflict, "RG1622", "maintenance request identity or authorization is invalid")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	result := JobResult{
		APIVersion: "rungrid/output/v1", ProjectID: layout.ProjectID,
		RequestID: request.RequestID, Operation: request.Operation,
		Success: runErr == nil, Data: encoded, FinishedAt: timestamp(),
	}
	if runErr != nil {
		result.ErrorCode, result.Diagnostic, result.Error = errs.Code(runErr), errs.Diagnostic(runErr), runErr.Error()
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	path := resultPath(layout, request.GenerationID, request.Operation, request.RequestID)
	return state.WriteFileAtomic(filepath.Dir(path), filepath.Base(path), append(content, '\n'), 0o600)
}

func WaitJobResult(ctx context.Context, layout state.Layout, request Request) (JobResult, error) {
	if !validRequest(request) || request.ProjectID != layout.ProjectID {
		return JobResult{}, errs.New(errs.ExitConflict, "RG1622", "maintenance request identity or authorization is invalid")
	}
	path := resultPath(layout, request.GenerationID, request.Operation, request.RequestID)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if content, err := readPrivateFile(path); err == nil {
			var result JobResult
			if json.Unmarshal(content, &result) != nil || result.APIVersion != "rungrid/output/v1" ||
				result.ProjectID != layout.ProjectID || result.RequestID != request.RequestID || result.Operation != request.Operation ||
				(result.Success && result.ErrorCode != 0) || (!result.Success && (result.ErrorCode == 0 || result.Error == "")) {
				return JobResult{}, errs.New(errs.ExitConflict, "RG1624", "maintenance result identity is invalid")
			}
			_ = os.Remove(path)
			return result, nil
		} else if !os.IsNotExist(err) {
			return JobResult{}, err
		}
		select {
		case <-ctx.Done():
			return JobResult{}, errs.Wrap(errs.ExitNotReady, "RG1625", "wait for repository maintenance result", ctx.Err())
		case <-ticker.C:
		}
	}
}

func CleanupClaim(path string) { _ = os.Remove(path) }

func CancelRequest(layout state.Layout, request Request) {
	if !validRequest(request) || request.ProjectID != layout.ProjectID {
		return
	}
	path := pendingRequestPath(layout, request.GenerationID, request.Operation)
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var current Request
	if json.Unmarshal(content, &current) == nil && current.RequestID == request.RequestID {
		_ = os.Remove(path)
	}
}

func ProcessName(operation string) string {
	if operation == OperationPrune {
		return PruneProcessName
	}
	return SyncProcessName
}

func pendingRequestPath(layout state.Layout, generationID, operation string) string {
	return filepath.Join(layout.ProjectDir, "maintenance", generationID+"-"+operation+".request.json")
}

func resultPath(layout state.Layout, generationID, operation, requestID string) string {
	return filepath.Join(layout.ProjectDir, "maintenance", fmt.Sprintf("%s-%s-%s.result.json", generationID, operation, requestID))
}

func requestIsExpired(request Request) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, request.ExpiresAt)
	return err != nil || time.Now().After(expiresAt)
}

func maintenanceRequestID() (string, error) {
	content := make([]byte, 12)
	if _, err := rand.Read(content); err != nil {
		return "", err
	}
	return hex.EncodeToString(content), nil
}

func validJobIdentity(generationID, operation string) bool {
	if len(generationID) != 20 || (operation != OperationSync && operation != OperationPrune) {
		return false
	}
	_, err := hex.DecodeString(generationID)
	return err == nil
}

func validRequest(request Request) bool {
	if request.APIVersion != "rungrid/output/v1" || request.ProjectID == "" ||
		!validJobIdentity(request.GenerationID, request.Operation) || len(request.RequestID) != 24 || !request.Authorized {
		return false
	}
	_, err := hex.DecodeString(request.RequestID)
	return err == nil
}

func readPrivateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errs.New(errs.ExitConflict, "RG1627", "maintenance journal is not a private regular file")
	}
	return os.ReadFile(path)
}
