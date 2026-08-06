package maintenance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/state"
)

func TestMaintenanceRequestIsSingleUseAndReturnsTypedResult(t *testing.T) {
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(layout, "0123456789abcdefabcd", OperationSync, []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(layout, request); err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(layout, request); err == nil {
		t.Fatal("overwrote a live pending request")
	}
	claimed, claimPath, err := ClaimRequest(layout, "0123456789abcdefabcd", OperationSync)
	if err != nil {
		t.Fatal(err)
	}
	defer CleanupClaim(claimPath)
	if claimed.RequestID != request.RequestID || len(claimed.Repositories) != 1 {
		t.Fatalf("unexpected claimed request: %#v", claimed)
	}
	if _, _, err := ClaimRequest(layout, "0123456789abcdefabcd", OperationSync); err == nil {
		t.Fatal("claimed request was replayable")
	}
	runErr := errs.New(errs.ExitPartial, "RGTEST", "partial test result")
	if err := WriteJobResult(layout, request, SyncReport{Operation: OperationSync}, runErr); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := WaitJobResult(ctx, layout, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.ErrorCode != errs.ExitPartial || result.Diagnostic != "RGTEST" {
		t.Fatalf("unexpected job result: %#v", result)
	}
}

func TestMaintenanceWorkerRejectsMissingAuthorization(t *testing.T) {
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ClaimRequest(layout, "0123456789abcdefabcd", OperationPrune); err == nil {
		t.Fatal("worker accepted a missing request")
	}
	entries, err := os.ReadDir(layout.ProjectDir + "/maintenance")
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing request changed maintenance state: entries=%v err=%v", entries, err)
	}
}

func TestMaintenanceWorkerRejectsPathLikeIdentity(t *testing.T) {
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ClaimRequest(layout, "../../outside", OperationSync); err == nil {
		t.Fatal("worker accepted a path-like generation")
	}
	if _, err := os.Stat(layout.ProjectDir); !os.IsNotExist(err) {
		t.Fatalf("invalid identity created project state: %v", err)
	}
}
