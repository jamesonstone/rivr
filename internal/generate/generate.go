package generate

import (
	"io/fs"
	"sort"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/planner"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/state"
	"github.com/jamesonstone/rungrid/internal/terminalshell"
	"github.com/jamesonstone/rungrid/internal/warp"
)

type Result struct {
	Plan      planner.Plan `json:"plan"`
	Directory string       `json:"directory"`
	Created   bool         `json:"created"`
}

func Run(loaded *manifest.Loaded, stateOverride, generatorVersion string, checkOnly bool) (Result, error) {
	plan := planner.Build(loaded, generatorVersion)
	layout, err := state.NewLayout(loaded.Manifest.Project.ID, stateOverride)
	if err != nil {
		return Result{}, err
	}
	if activeGeneration, active, err := state.RecordedRuntimeGeneration(layout); err != nil {
		return Result{}, err
	} else if active && activeGeneration != plan.GenerationID {
		return Result{}, errs.New(errs.ExitConflict, "RG402", "a different runtime generation is active; run rungrid down before regenerating")
	}
	compiled, err := processcompose.Compile(&loaded.Manifest, plan.GenerationID)
	if err != nil {
		return Result{}, err
	}
	planJSON, err := plan.JSON()
	if err != nil {
		return Result{}, errs.Wrap(errs.ExitFailure, "RG401", "encode generation plan", err)
	}
	builder := state.NewBuilder(layout, plan.GenerationID, generatorVersion)
	if err := builder.Add("manifest.yaml", "normalized-manifest", loaded.MergedYAML, 0o600); err != nil {
		return Result{}, err
	}
	if err := builder.Add("plan.json", "plan", planJSON, 0o600); err != nil {
		return Result{}, err
	}
	if err := builder.Add("process-compose.yaml", "process-compose-config", compiled.Configuration, 0o600); err != nil {
		return Result{}, err
	}
	wrapperNames := make([]string, 0, len(compiled.Wrappers))
	for name := range compiled.Wrappers {
		wrapperNames = append(wrapperNames, name)
	}
	sort.Strings(wrapperNames)
	for _, name := range wrapperNames {
		if err := builder.Add("wrappers/"+name, "runtime-wrapper", compiled.Wrappers[name], 0o700); err != nil {
			return Result{}, err
		}
	}
	if loaded.Manifest.Terminal.Mode == "warp" {
		for _, template := range warp.Templates(&loaded.Manifest, plan.GenerationID) {
			if err := builder.Add("terminal/warp/"+template.Filename, "warp-tab-template", template.Content, 0o600); err != nil {
				return Result{}, err
			}
		}
		for _, artifact := range terminalshell.Generate(&loaded.Manifest, plan.GenerationID) {
			if err := builder.Add(artifact.Path, artifact.Kind, artifact.Content, fs.FileMode(artifact.Mode)); err != nil {
				return Result{}, err
			}
		}
	}
	directory, created, err := builder.Promote(checkOnly)
	if err != nil {
		return Result{Plan: plan, Directory: directory, Created: created}, err
	}
	return Result{Plan: plan, Directory: directory, Created: created}, nil
}
