package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveMarkerFromNestedDirectory(t *testing.T) {
	t.Setenv("CFS_WORKSPACE_ROOT", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, MarkerName), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(nested)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	wantRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != wantRoot {
		t.Fatalf("Root = %q, want %q", got.Root, wantRoot)
	}
	if got.Source != "marker" {
		t.Fatalf("Source = %q, want marker", got.Source)
	}
	if len(got.ID) != 64 {
		t.Fatalf("ID length = %d, want 64", len(got.ID))
	}
}

func TestExplicitRootTakesPrecedence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CFS_WORKSPACE_ROOT", root)

	got, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	wantRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != wantRoot || got.Source != "environment" {
		t.Fatalf("Resolve() = %#v", got)
	}
}

func TestIdentityIsStableAndRootSpecific(t *testing.T) {
	first := identify(filepath.Clean("/tmp/a"), "test")
	again := identify(filepath.Clean("/tmp/a"), "test")
	second := identify(filepath.Clean("/tmp/b"), "test")

	if first.ID != again.ID {
		t.Fatal("same root produced different IDs")
	}
	if first.ID == second.ID {
		t.Fatal("different roots produced the same ID")
	}
}

func TestResolveUsesDistinctContextsForGitWorktrees(t *testing.T) {
	t.Setenv("CFS_WORKSPACE_ROOT", "")
	root := t.TempDir()
	primary := filepath.Join(root, "primary")
	secondary := filepath.Join(root, "secondary")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-q", primary)
	runGit(t, "-C", primary, "-c", "user.name=cfs-test", "-c", "user.email=cfs-test@example.invalid", "commit", "--allow-empty", "-q", "-m", "initial")
	runGit(t, "-C", primary, "worktree", "add", "-q", "-b", "secondary", secondary)

	first, err := Resolve(primary)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(secondary)
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "git-worktree" || second.Source != "git-worktree" {
		t.Fatalf("sources = %q / %q, want git-worktree", first.Source, second.Source)
	}
	if first.Root == second.Root {
		t.Fatalf("worktrees resolved to the same root %q", first.Root)
	}
	if first.ID == second.ID {
		t.Fatalf("worktrees resolved to the same context %q", first.ID)
	}
}

func TestResolveFailsOutsideWorkspace(t *testing.T) {
	t.Setenv("CFS_WORKSPACE_ROOT", "")
	_, err := Resolve(t.TempDir())
	if err == nil {
		t.Fatal("Resolve() error = nil, want an error")
	}
}

func TestResolveRejectsUnsupportedMarkerVersion(t *testing.T) {
	t.Setenv("CFS_WORKSPACE_ROOT", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, MarkerName), []byte("version = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root); err == nil {
		t.Fatal("Resolve() error = nil, want unsupported marker error")
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
