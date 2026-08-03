package workspace

import (
	"encoding/json"

	"github.com/jamesonstone/rungrid/internal/manifest"
	"github.com/jamesonstone/rungrid/internal/state"
)

func LifecycleDigest(lifecycle manifest.Lifecycle) string {
	content, _ := json.Marshal(lifecycle)
	return state.Hash(content)
}
