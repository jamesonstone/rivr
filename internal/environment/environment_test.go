package environment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestResolveProviderOrderAndExplicitValues(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("A=dotenv\nB=dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(root, "provider")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf 'B=command\\nC=command\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := &manifest.Service{
		WorkingDirectory: ".",
		Environment: manifest.Environment{
			Values: map[string]string{"C": "explicit", "D": "explicit"},
			Providers: []manifest.EnvironmentProvider{
				{Type: "dotenv", Path: ".env", Timeout: manifest.Duration{Duration: 5 * time.Second}},
				{Type: "command", Argv: []string{provider}, Timeout: manifest.Duration{Duration: 5 * time.Second}},
			},
		},
	}
	_, values, err := Resolve(context.Background(), service, root)
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"A": "dotenv", "B": "command", "C": "explicit", "D": "explicit"} {
		if values[key] != expected {
			t.Errorf("%s=%q, want %q", key, values[key], expected)
		}
	}
}

func TestCommandProviderFailureRedactsOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	provider := filepath.Join(root, "provider")
	secret := "do-not-expose-provider-output"
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf '"+secret+"' >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := &manifest.Service{
		WorkingDirectory: ".",
		Environment:      manifest.Environment{Providers: []manifest.EnvironmentProvider{{Type: "command", Argv: []string{provider}, Timeout: manifest.Duration{Duration: time.Second}}}},
	}
	_, _, err := Resolve(context.Background(), service, root)
	if err == nil {
		t.Fatal("expected provider failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("provider output leaked in error: %v", err)
	}
}

func TestResolveRejectsProviderSymlinkEscapeAtExecutionTime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, ".env"), []byte("A=outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	service := &manifest.Service{
		WorkingDirectory: ".",
		Environment: manifest.Environment{Providers: []manifest.EnvironmentProvider{{
			Type: "dotenv", Path: "escaped/.env", Timeout: manifest.Duration{Duration: time.Second},
		}}},
	}
	_, _, err := Resolve(context.Background(), service, root)
	if err == nil || !strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("expected execution-time provider boundary rejection, got %v", err)
	}
}

func FuzzParseDotenvNeverAcceptsNULKey(f *testing.F) {
	f.Add("A=value\n")
	f.Add("export B='value'\n")
	f.Add("BAD KEY=value\n")
	f.Fuzz(func(t *testing.T, input string) {
		values, err := parseDotenv([]byte(input))
		if err != nil {
			return
		}
		for key := range values {
			if strings.ContainsRune(key, '\x00') || !validKey(key) {
				t.Fatalf("accepted invalid key %q", key)
			}
		}
	})
}
