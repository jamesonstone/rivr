//go:build darwin || linux

package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/generate"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/serviceexec"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/supervisor"
	"github.com/jamesonstone/rungrid/internal/warp"
	"gopkg.in/yaml.v3"
)

type Active struct {
	Layout   state.Layout
	Runtime  supervisor.Runtime
	Manifest *manifest.Manifest
}

type UpOptions struct {
	StateOverride    string
	GeneratorVersion string
	Headless         bool
	Open             bool
	Requested        []string
}

type UpResult struct {
	Generation string `json:"generation"`
	RuntimePID int    `json:"runtime_pid"`
	Socket     string `json:"socket"`
	Reused     bool   `json:"reused"`
	OpenedWarp bool   `json:"opened_warp"`
}

type ServiceStatus struct {
	Name          string `json:"name"`
	Source        string `json:"source"`
	Activation    string `json:"activation"`
	Status        string `json:"status"`
	Health        string `json:"health,omitempty"`
	PID           int    `json:"pid,omitempty"`
	ExitCode      int    `json:"exit_code,omitempty"`
	SessionOwned  bool   `json:"session_owned"`
	TabRegistered bool   `json:"tab_registered"`
}

func LoadActive(ctx context.Context, projectID, stateOverride string) (Active, error) {
	if projectID == "" {
		return Active{}, errs.New(errs.ExitUsage, "RG1101", "project id is required to select active state")
	}
	layout, err := state.NewLayout(projectID, stateOverride)
	if err != nil {
		return Active{}, err
	}
	runtimeState, err := supervisor.Read(layout)
	if err != nil {
		if os.IsNotExist(err) {
			return Active{}, errs.New(errs.ExitConflict, "RG1102", "no active Rungrid runtime")
		}
		return Active{}, err
	}
	if err := supervisor.Verify(ctx, layout, runtimeState); err != nil {
		return Active{}, err
	}
	manifestPath := filepath.Join(layout.ProjectDir, "generations", runtimeState.GenerationID, "manifest.yaml")
	generatedManifest, err := manifest.LoadGenerated(manifestPath, runtimeState.WorkspaceRoot)
	if err != nil {
		return Active{}, err
	}
	return Active{Layout: layout, Runtime: runtimeState, Manifest: generatedManifest}, nil
}

func Up(ctx context.Context, loaded *manifest.Loaded, options UpOptions) (UpResult, error) {
	effective := loaded
	if options.Headless && loaded.Manifest.Terminal.Mode != "headless" {
		copyLoaded := *loaded
		copyLoaded.Manifest = loaded.Manifest
		copyLoaded.Manifest.Terminal.Mode = "headless"
		copyLoaded.Manifest.Terminal.Open = manifest.Bool(false)
		content, err := yaml.Marshal(copyLoaded.Manifest)
		if err != nil {
			return UpResult{}, errs.Wrap(errs.ExitFailure, "RG1126", "encode headless manifest override", err)
		}
		copyLoaded.MergedYAML = content
		effective = &copyLoaded
	}
	generated, err := generate.Run(effective, options.StateOverride, options.GeneratorVersion, false)
	if err != nil {
		return UpResult{}, err
	}
	for i := range effective.Manifest.Services {
		service := &effective.Manifest.Services[i]
		if service.Source != "external" {
			continue
		}
		waitContext, cancel := context.WithTimeout(ctx, effective.Manifest.Runtime.StartupTimeout.Duration)
		err := serviceexec.WaitExternal(waitContext, effective.Root, service)
		cancel()
		if err != nil {
			return UpResult{}, err
		}
	}
	pcExecutable, err := exec.LookPath(effective.Manifest.Runtime.ProcessCompose.Executable)
	if err != nil {
		return UpResult{}, errs.Wrap(errs.ExitDependency, "RG1103", "resolve Process Compose executable", err)
	}
	pcVersion, err := processcompose.Version(ctx, pcExecutable)
	if err != nil {
		return UpResult{}, err
	}
	rungridExecutable, err := processcompose.ExecutablePath()
	if err != nil {
		return UpResult{}, errs.Wrap(errs.ExitFailure, "RG1104", "resolve Rungrid executable", err)
	}
	layout, err := state.NewLayout(effective.Manifest.Project.ID, options.StateOverride)
	if err != nil {
		return UpResult{}, err
	}
	runtimeState, reused, err := supervisor.Start(ctx, supervisor.StartOptions{
		Layout: layout, GenerationID: generated.Plan.GenerationID, WorkspaceRoot: effective.Root,
		ProcessCompose: pcExecutable, ProcessComposeVersion: pcVersion, RungridExecutable: rungridExecutable,
		StartupTimeout: effective.Manifest.Runtime.StartupTimeout.Duration,
	})
	if err != nil {
		return UpResult{}, err
	}
	client := supervisor.Client(layout, runtimeState)
	readyContext, cancel := context.WithTimeout(ctx, effective.Manifest.Runtime.StartupTimeout.Duration)
	_, readyErr := client.Run(readyContext, "project", "is-ready", "--wait")
	cancel()
	if readyErr != nil {
		return UpResult{}, errs.Wrap(errs.ExitNotReady, "RG1105", "workspace-owned services did not become ready", readyErr)
	}

	opened := false
	shouldOpen := options.Open && !options.Headless && effective.Manifest.Terminal.Mode == "warp"
	if shouldOpen {
		record, installErr := warp.Install(layout, &effective.Manifest, generated.Plan.GenerationID, rungridExecutable)
		if installErr != nil {
			return UpResult{}, installErr
		}
		if openErr := warp.Open(ctx, record, ""); openErr != nil {
			return UpResult{}, openErr
		}
		opened = true
	}
	for _, name := range options.Requested {
		service, exists := manifest.FindService(&effective.Manifest, name)
		if !exists {
			return UpResult{}, errs.New(errs.ExitUsage, "RG1106", "unknown requested service: "+name)
		}
		if service.Activation == "tab" && !shouldOpen {
			return UpResult{}, errs.New(errs.ExitUsage, "RG1107", "requested tab service requires Warp opening or a separate rungrid session")
		}
		waitContext, waitCancel := context.WithTimeout(ctx, effective.Manifest.Runtime.StartupTimeout.Duration)
		err := waitForService(waitContext, client, layout, runtimeState.GenerationID, service)
		waitCancel()
		if err != nil {
			return UpResult{}, err
		}
	}
	return UpResult{Generation: runtimeState.GenerationID, RuntimePID: runtimeState.PID, Socket: runtimeState.Socket, Reused: reused, OpenedWarp: opened}, nil
}
