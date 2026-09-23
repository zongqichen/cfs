package cfhome

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestImportCopiesPrivateConfiguration(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	want := []byte(`{"Target":"https://api.example.com","RefreshToken":"secret"}`)
	writeConfig(t, source, want, 0o600)

	if err := Import(source, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(ConfigPath(destination))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("imported configuration = %q, want %q", got, want)
	}
	assertMode(t, filepath.Dir(ConfigPath(destination)), 0o700)
	assertMode(t, ConfigPath(destination), 0o600)
}

func TestImportRejectsBroadSourcePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not authoritative on Windows")
	}
	source := t.TempDir()
	writeConfig(t, source, []byte(`{}`), 0o644)

	if err := Import(source, t.TempDir()); err == nil {
		t.Fatal("Import() error = nil, want broad-permission rejection")
	}
	assertMode(t, ConfigPath(source), 0o644)
}

func TestImportRejectsSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ConfigPath(source)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ConfigPath(source)); err != nil {
		t.Fatal(err)
	}

	if err := Import(source, t.TempDir()); err == nil {
		t.Fatal("Import() error = nil, want symbolic-link rejection")
	}
}

func TestImportRejectsInvalidJSON(t *testing.T) {
	source := t.TempDir()
	writeConfig(t, source, []byte(`not-json`), 0o600)

	if err := Import(source, t.TempDir()); err == nil {
		t.Fatal("Import() error = nil, want invalid JSON rejection")
	}
}

func TestHasConfigRejectsSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ConfigPath(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ConfigPath(home)); err != nil {
		t.Fatal(err)
	}

	if _, err := HasConfig(home); err == nil {
		t.Fatal("HasConfig() error = nil, want symbolic-link rejection")
	}
}

func TestHasTarget(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "targeted", content: "{\"Target\":\"https://api.example.com\"}", want: true},
		{name: "blank target", content: "{\"Target\":\"  \"}"},
		{name: "no target", content: "{}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			writeConfig(t, home, []byte(test.content), 0o600)
			got, err := HasTarget(home)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("HasTarget() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestHasTargetReturnsFalseWhenConfigurationIsMissing(t *testing.T) {
	got, err := HasTarget(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("HasTarget() = true without a configuration")
	}
}

func writeConfig(t *testing.T, home string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(ConfigPath(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(home), content, mode); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %s = %o, want %o", path, got, want)
	}
}
