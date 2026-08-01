package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilderPromotesAndReusesIdenticalGeneration(t *testing.T) {
	t.Parallel()
	layout, err := NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(layout, "0123456789abcdef", "test")
	if err := builder.Add("config/file.txt", "fixture", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, created, err := builder.Promote(false)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a new generation")
	}
	info, err := os.Stat(filepath.Join(directory, "config", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact mode is %o", info.Mode().Perm())
	}
	_, created, err = builder.Promote(false)
	if err != nil || created {
		t.Fatalf("expected an identical reuse, created=%v err=%v", created, err)
	}
}

func TestBuilderRejectsModifiedGeneration(t *testing.T) {
	t.Parallel()
	layout, err := NewLayout("example-k7m4q2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(layout, "0123456789abcdef", "test")
	if err := builder.Add("file.txt", "fixture", []byte("owned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, _, err := builder.Promote(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file.txt"), []byte("modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = builder.Promote(false)
	if err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
}

func TestAtomicWriteRejectsEscapeAndSymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WriteFileAtomic(base, "../escape", []byte("no"), 0o600); err == nil {
		t.Fatal("expected path escape rejection")
	}
	target := filepath.Join(base, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(base, "link", []byte("replacement"), 0o600); err == nil {
		t.Fatal("expected symlink replacement rejection")
	}
}

func TestProjectIDNeverUsesWorkspacePath(t *testing.T) {
	t.Parallel()
	one := Hash([]byte("manifest"), []byte("version"))
	two := Hash([]byte("manifest"), []byte("version"))
	if one != two {
		t.Fatal("deterministic hash changed")
	}
	if strings.Contains(one, string(filepath.Separator)) {
		t.Fatalf("hash contains a path separator: %s", one)
	}
}

func TestLayoutRejectsSymlinkedStateComponent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "rungrid")); err != nil {
		t.Fatal(err)
	}
	layout, err := NewLayout("example-k7m4q2", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}
