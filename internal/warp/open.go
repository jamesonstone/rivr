package warp

import (
	"context"
	"os/exec"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
)

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
