package processcompose

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/subprocess"
)

func Version(ctx context.Context, executable string) (string, error) {
	versionContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := subprocess.Combined(exec.CommandContext(versionContext, executable, "version"))
	if err != nil {
		return "", errs.Wrap(errs.ExitDependency, "RG306", "read Process Compose version", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Version:") {
			version := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Version:"))
			if !SupportedVersion(version) {
				return "", errs.New(errs.ExitDependency, "RG307", fmt.Sprintf("Process Compose %s is outside >=1.120.0,<2.0.0", version))
			}
			return version, nil
		}
	}
	return "", errs.New(errs.ExitDependency, "RG308", "Process Compose version line was missing")
}

func SupportedVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.SplitN(version, ".", 4)
	if len(parts) < 3 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	patchText := parts[2]
	if index := strings.IndexFunc(patchText, func(r rune) bool { return r < '0' || r > '9' }); index >= 0 {
		patchText = patchText[:index]
	}
	patch, patchErr := strconv.Atoi(patchText)
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return false
	}
	return major == 1 && (minor > 120 || (minor == 120 && patch >= 0))
}
