package terminalshell

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

type Artifact struct {
	Path    string
	Kind    string
	Content []byte
	Mode    uint32
}

func Generate(m *manifest.Manifest, generationID string) []Artifact {
	var result []Artifact
	for _, service := range m.Services {
		if service.Activation != "tab" || service.Source == "external" || len(service.Terminal.TriggerArgv) == 0 {
			continue
		}
		directory := filepath.ToSlash(filepath.Join("terminal", "shell", service.Name))
		executable := service.Terminal.TriggerArgv[0]
		zshrc := fmt.Sprintf(`if [[ -r "$RUNGRID_USER_ZDOTDIR/.zshrc" && "$RUNGRID_USER_ZDOTDIR" != "$ZDOTDIR" ]]; then
  rungrid_managed_zdotdir=$ZDOTDIR
  ZDOTDIR=$RUNGRID_USER_ZDOTDIR
  source "$ZDOTDIR/.zshrc"
  ZDOTDIR=$rungrid_managed_zdotdir
  unset rungrid_managed_zdotdir
fi

if [[ -z "${HISTFILE:-}" || "$HISTFILE" == "$ZDOTDIR/.zsh_history" ]]; then
  HISTFILE="$RUNGRID_USER_ZDOTDIR/.zsh_history"
fi

export RUNGRID_ORIGINAL_PATH="$PATH"
export PATH="$RUNGRID_SHIM_DIR:$PATH"
unalias %s 2>/dev/null || true
function %s() {
  "$RUNGRID_SHIM_DIR/%s" "$@"
}

if [[ "${RUNGRID_SHELL_BOOTSTRAPPED:-0}" != "1" ]]; then
  export RUNGRID_SHELL_BOOTSTRAPPED=1
  "$RUNGRID_EXECUTABLE" --state-dir "$RUNGRID_STATE_DIR" --project "$RUNGRID_PROJECT_ID" session "$RUNGRID_SERVICE"
fi
`, executable, executable, executable)
		shim := `#!/bin/sh
set -eu
exec "$RUNGRID_EXECUTABLE" --state-dir "$RUNGRID_STATE_DIR" --project "$RUNGRID_PROJECT_ID" internal trigger --generation "$RUNGRID_GENERATION_ID" --service "$RUNGRID_SERVICE" -- "$@"
`
		result = append(result,
			Artifact{Path: directory + "/.zshrc", Kind: "managed-zsh-config", Content: []byte(zshrc), Mode: 0o600},
			Artifact{Path: directory + "/bin/" + strings.ReplaceAll(executable, "/", "_"), Kind: "trigger-shim", Content: []byte(shim), Mode: 0o700},
		)
	}
	return result
}
