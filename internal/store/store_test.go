package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zongqichen/cfs/internal/workspace"
)

func TestEnsureCreatesPrivateContextAndMetadata(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	s := New(root)
	s.Now = func() time.Time { return now }
	ws := workspace.Workspace{
		Root:        filepath.Join(root, "project"),
		Source:      "test",
		ID:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Fingerprint: "fingerprint",
	}
	if err := os.MkdirAll(ws.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, err := s.ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ensure(ctx, ws); err != nil {
		t.Fatal(err)
	}

	metadata, err := s.ReadMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Workspace != ws.Root || !metadata.CreatedAt.Equal(now) {
		t.Fatalf("metadata = %#v", metadata)
	}
	if info, err := os.Stat(ctx.CFHome); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("CF home permissions = %o, want 700", info.Mode().Perm())
	}
}

func TestEnsureRejectsMetadataMismatch(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ws := workspace.Workspace{
		Root:        filepath.Join(root, "project"),
		Source:      "test",
		ID:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Fingerprint: "first",
	}
	if err := os.MkdirAll(ws.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, _ := s.ContextFor(ws)
	if err := s.Ensure(ctx, ws); err != nil {
		t.Fatal(err)
	}

	ws.Fingerprint = "second"
	if err := s.Ensure(ctx, ws); err == nil {
		t.Fatal("Ensure() error = nil, want metadata mismatch")
	}
}

func TestMoveToTrashIsRecoverable(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ws := workspace.Workspace{
		Root: filepath.Join(root, "project"), Source: "test",
		ID: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	if err := os.MkdirAll(ws.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, _ := s.ContextFor(ws)
	if err := s.Ensure(ctx, ws); err != nil {
		t.Fatal(err)
	}

	destination, err := s.MoveToTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("trash destination: %v", err)
	}
	if _, err := os.Stat(ctx.Dir); !os.IsNotExist(err) {
		t.Fatalf("original context still exists: %v", err)
	}
}

func TestPrepareRejectsLinkedContextsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, contextsDirectory)); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	ctx, err := s.Context("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Prepare(ctx); err == nil {
		t.Fatal("Prepare() error = nil, want symbolic-link rejection")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("target permissions = %o, want 755", info.Mode().Perm())
	}
	if entries, err := os.ReadDir(target); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("linked target was modified: %v", entries)
	}
}

func TestReadMetadataRejectsSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	s := New(root)
	ctx, err := s.Context("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ctx.MetadataPath); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReadMetadata(ctx); err == nil {
		t.Fatal("ReadMetadata() error = nil, want symbolic-link rejection")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("target permissions = %o, want 644", info.Mode().Perm())
	}
}
