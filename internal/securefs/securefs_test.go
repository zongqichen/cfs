package securefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONAtomicCreatesPrivateNestedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "state.json")
	want := map[string]string{"target": "development"}

	if err := WriteJSONAtomic(path, want); err != nil {
		t.Fatal(err)
	}

	assertPermissions(t, filepath.Dir(path), DirectoryMode)
	assertPermissions(t, path, FileMode)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["target"] != want["target"] {
		t.Fatalf("decoded target = %q, want %q", got["target"], want["target"])
	}
}

func TestWriteJSONAtomicReplacesFileWithoutTemporaryFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.json")
	if err := WriteJSONAtomic(path, map[string]int{"revision": 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONAtomic(path, map[string]int{"revision": 2}); err != nil {
		t.Fatal(err)
	}

	assertPermissions(t, path, FileMode)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{\n  \"revision\": 2\n}\n" {
		t.Fatalf("file contents = %q", raw)
	}
	if matches, err := filepath.Glob(filepath.Join(directory, "cfs-*.tmp")); err != nil {
		t.Fatal(err)
	} else if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %s = %o, want %o", path, got, want)
	}
}
