package terminalshell

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestGenerateExactTriggerInterception(t *testing.T) {
	t.Parallel()
	loaded, err := manifest.Load(filepath.Join("..", "..", "testdata", "example", ".rungrid.yaml"), "")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := Generate(&loaded.Manifest, "generation")
	if len(artifacts) != 4 {
		t.Fatalf("got %d shell artifacts", len(artifacts))
	}
	var apiConfig, apiShim string
	for _, artifact := range artifacts {
		if artifact.Path == "terminal/shell/api/.zshrc" {
			apiConfig = string(artifact.Content)
		}
		if artifact.Path == "terminal/shell/api/bin/make" {
			apiShim = string(artifact.Content)
		}
	}
	if !strings.Contains(apiConfig, "function make()") || !strings.Contains(apiConfig, `"$RUNGRID_SHIM_DIR/make" "$@"`) {
		t.Fatalf("api zsh wrapper is incomplete:\n%s", apiConfig)
	}
	if !strings.Contains(apiShim, "internal trigger") || !strings.Contains(apiShim, `-- "$@"`) {
		t.Fatalf("api trigger shim is incomplete:\n%s", apiShim)
	}
}

func TestArgumentVectorsMustMatchExactly(t *testing.T) {
	t.Parallel()
	if !equalArguments([]string{"dev"}, []string{"dev"}) {
		t.Fatal("equal trigger did not match")
	}
	for _, arguments := range [][]string{{}, {"dev", "extra"}, {"other"}} {
		if equalArguments([]string{"dev"}, arguments) {
			t.Fatalf("non-exact arguments matched: %#v", arguments)
		}
	}
}
