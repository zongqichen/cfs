package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/contextname"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/workspace"
)

func TestContextForNameUsesWorkspaceIdentityForDefaultAndSeparatesNames(t *testing.T) {
	s := New(t.TempDir())
	ws := workspace.Workspace{ID: strings.Repeat("a", 64)}

	defaultContext, err := s.ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	prod, err := s.ContextForName(ws, "prod")
	if err != nil {
		t.Fatal(err)
	}
	poc, err := s.ContextForName(ws, "poc")
	if err != nil {
		t.Fatal(err)
	}

	if defaultContext.ID != ws.ID || defaultContext.Name != contextname.Default {
		t.Fatalf("default context = %#v", defaultContext)
	}
	if prod.ID == defaultContext.ID || prod.ID == poc.ID {
		t.Fatalf("named context IDs are not isolated: default=%s prod=%s poc=%s", defaultContext.ID, prod.ID, poc.ID)
	}
	again, err := s.ContextForName(ws, "prod")
	if err != nil || again.ID != prod.ID {
		t.Fatalf("named context identity is unstable: %#v, %v", again, err)
	}
	otherWorkspace := workspace.Workspace{ID: strings.Repeat("b", 64)}
	otherProd, err := s.ContextForName(otherWorkspace, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if otherProd.ID == prod.ID {
		t.Fatalf("different workspaces shared named context ID %s", prod.ID)
	}
}

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

func TestReadMetadataRejectsIncompleteIdentity(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ws := workspace.Workspace{
		Root: filepath.Join(root, "project"), Source: "test",
		ID: strings.Repeat("c", 64), Fingerprint: "fingerprint",
	}
	if err := os.MkdirAll(ws.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, err := s.ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	metadata := Metadata{
		Version:     metadataVersion,
		ContextID:   ctx.ID,
		ContextName: contextname.Default,
		WorkspaceID: ws.ID,
		Workspace:   ws.Root,
		Source:      ws.Source,
		Fingerprint: ws.Fingerprint,
		CreatedAt:   time.Now().UTC(),
		LastUsedAt:  time.Now().UTC(),
	}
	tests := []struct {
		name   string
		mutate func(*Metadata)
		want   string
	}{
		{name: "context name", mutate: func(value *Metadata) { value.ContextName = "" }, want: "missing context name"},
		{name: "workspace ID", mutate: func(value *Metadata) { value.WorkspaceID = "" }, want: "missing workspace ID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			incomplete := metadata
			test.mutate(&incomplete)
			if err := os.WriteFile(ctx.MetadataPath, mustJSON(t, incomplete), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadMetadata(ctx); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadMetadata() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadMetadataRejectsNamedContextWithMismatchedIdentity(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ws := workspace.Workspace{
		Root: filepath.Join(root, "project"), Source: "test",
		ID: strings.Repeat("d", 64), Fingerprint: "fingerprint",
	}
	if err := os.MkdirAll(ws.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, err := s.ContextForName(ws, "prod")
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
	metadata.ContextName = "poc"
	if err := os.WriteFile(ctx.MetadataPath, mustJSON(t, metadata), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReadMetadata(ctx); err == nil {
		t.Fatal("ReadMetadata() error = nil, want context identity mismatch")
	}
}

func TestListForWorkspaceReturnsOnlyItsNamedContexts(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	first := workspace.Workspace{
		Root: filepath.Join(root, "first"), Source: "test",
		ID: strings.Repeat("1", 64), Fingerprint: "first",
	}
	second := workspace.Workspace{
		Root: filepath.Join(root, "second"), Source: "test",
		ID: strings.Repeat("2", 64), Fingerprint: "second",
	}
	for _, ws := range []workspace.Workspace{first, second} {
		if err := os.MkdirAll(ws.Root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct {
		workspace workspace.Workspace
		name      string
	}{
		{workspace: first, name: contextname.Default},
		{workspace: first, name: "prod"},
		{workspace: second, name: "prod"},
	} {
		ctx, err := s.ContextForName(fixture.workspace, fixture.name)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Ensure(ctx, fixture.workspace); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := s.ListForWorkspace(first)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Context.Name] = true
	}
	if len(names) != 2 || !names[contextname.Default] || !names["prod"] {
		t.Fatalf("ListForWorkspace() names = %v", names)
	}
}

func TestListSkipsIncompleteContextWithoutHidingValidEntries(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ws := workspace.Workspace{
		Root: filepath.Join(root, "project"), Source: "test",
		ID: strings.Repeat("3", 64), Fingerprint: "fingerprint",
	}
	if err := os.MkdirAll(ws.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	valid, err := s.ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ensure(valid, ws); err != nil {
		t.Fatal(err)
	}
	incomplete, err := s.ContextForName(ws, "interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prepare(incomplete); err != nil {
		t.Fatal(err)
	}

	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Context.ID != valid.ID {
		t.Fatalf("List() = %#v, want only the valid context", entries)
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

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
