package store

import (
	"os"
	"path/filepath"
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
	} else if info.Mode().Perm() != 0o700 {
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
