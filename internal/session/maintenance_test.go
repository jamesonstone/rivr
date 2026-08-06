//go:build darwin || linux

package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/state"
)

func TestMaintenancePauseResumePreservesSessionOwnership(t *testing.T) {
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(layout, "generation-one", "api", "tab-one")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release session lock: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type pauseResult struct {
		handle *MaintenanceHandle
		err    error
	}
	paused := make(chan pauseResult, 1)
	go func() {
		handle, pauseErr := Pause(ctx, layout, "generation-one", "api", 2*time.Second)
		paused <- pauseResult{handle: handle, err: pauseErr}
	}()
	request := waitForRequest(t, ctx, layout, lock.identity, "pause")
	if err := acknowledgeMaintenance(layout, request, "paused", nil); err != nil {
		t.Fatal(err)
	}
	pause := <-paused
	if pause.err != nil {
		t.Fatal(pause.err)
	}
	if _, live := Active(layout, "generation-one", "api"); !live {
		t.Fatal("session registration disappeared while paused")
	}
	if _, err := Acquire(layout, "generation-one", "api", "tab-two"); err == nil || !strings.Contains(err.Error(), "already has an owning session") {
		t.Fatalf("paused session lost exclusive ownership: %v", err)
	}
	resumed := make(chan error, 1)
	go func() { resumed <- pause.handle.Resume(ctx) }()
	request = waitForRequest(t, ctx, layout, lock.identity, "resume")
	if err := acknowledgeMaintenance(layout, request, "running", nil); err != nil {
		t.Fatal(err)
	}
	if err := <-resumed; err != nil {
		t.Fatal(err)
	}
}

func waitForRequest(t *testing.T, ctx context.Context, layout state.Layout, identity Registration, action string) maintenanceRequest {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if request, exists := readMaintenanceRequest(layout, identity); exists && request.Action == action {
			return request
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s request", action)
		case <-ticker.C:
		}
	}
}

func TestExpiredMaintenanceRequestIsIgnoredAndCleanedByIdentity(t *testing.T) {
	layout, err := state.NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(layout, "generation-one", "api", "tab-one")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release session lock: %v", err)
		}
	})
	request := maintenanceRequest{
		APIVersion: "rungrid/output/v1", ProjectID: layout.ProjectID,
		GenerationID: lock.identity.GenerationID, Service: lock.identity.Service,
		RequestID: "expired", SessionPID: lock.identity.PID,
		SessionIdentity: lock.identity.ProcessIdentity, Action: "pause",
		ExpiresAt: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano),
	}
	if err := writeMaintenanceRequest(layout, request); err != nil {
		t.Fatal(err)
	}
	if _, exists := readMaintenanceRequest(layout, lock.identity); exists {
		t.Fatal("expired maintenance request remained actionable")
	}
	removeMaintenanceTransition(layout, request)
	if _, exists := readMaintenanceRequestFile(layout, request.GenerationID, request.Service); exists {
		t.Fatal("expired maintenance request was not removed")
	}

	newer := request
	newer.RequestID = "newer"
	newer.ExpiresAt = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := writeMaintenanceRequest(layout, newer); err != nil {
		t.Fatal(err)
	}
	removeMaintenanceTransition(layout, request)
	current, exists := readMaintenanceRequestFile(layout, request.GenerationID, request.Service)
	if !exists || current.RequestID != newer.RequestID {
		t.Fatal("cleanup removed a newer maintenance request")
	}
}
