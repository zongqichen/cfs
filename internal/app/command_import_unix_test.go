//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zongqichen/cfs/internal/cfhome"
	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/store"
	"github.com/zongqichen/cfs/internal/workspace"
)

func TestImportCopiesGlobalContextIntoWorkspace(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	want := []byte(`{"Target":"https://api.example.com","RefreshToken":"secret"}`)
	writeCFConfig(t, globalHome, want)
	root := markerWorkspace(t)

	result := runFromDirectory(t, root, []string{"cfs", "import", "--yes"})
	if result.code != exitOK {
		t.Fatalf("exit code = %d; stderr = %q", result.code, result.stderr)
	}
	if strings.Contains(result.stdout+result.stderr, "secret") {
		t.Fatalf("import output exposed credentials: %#v", result)
	}

	ws, err := workspace.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := store.New(stateRoot).ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cfhome.ConfigPath(ctx.CFHome))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("imported configuration = %q, want %q", got, want)
	}
	sourceAfter, err := os.ReadFile(cfhome.ConfigPath(globalHome))
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceAfter) != string(want) {
		t.Fatalf("global configuration changed: %q", sourceAfter)
	}
	if info, err := os.Stat(cfhome.ConfigPath(ctx.CFHome)); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("imported configuration permissions = %o, want 600", info.Mode().Perm())
	}

	target := runFromDirectory(t, root, []string{"cf", "target"})
	if target.code != exitOK {
		t.Fatalf("imported target is unavailable: %#v", target)
	}
}

func TestImportRequiresYesWithoutTerminal(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"global"}`))

	result := runFromDirectory(t, markerWorkspace(t), []string{"cfs", "import"})
	if result.code != exitUsage || !strings.Contains(result.stderr, "import requires --yes") {
		t.Fatalf("result = %#v", result)
	}
}

func TestImportRefusesToReplaceActiveTargetWithoutForce(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"first"}`))
	root := markerWorkspace(t)

	first := runFromDirectory(t, root, []string{"cfs", "import", "--yes"})
	if first.code != exitOK {
		t.Fatalf("first import: %#v", first)
	}
	writeCFConfig(t, globalHome, []byte(`{"Target":"second"}`))

	refused := runFromDirectory(t, root, []string{"cfs", "import"})
	if refused.code != exitUsage || !strings.Contains(refused.stderr, "use --force") {
		t.Fatalf("second import = %#v", refused)
	}

	forced := runFromDirectory(t, root, []string{"cfs", "import", "--yes", "--force"})
	if forced.code != exitOK {
		t.Fatalf("forced import = %#v", forced)
	}
	ws, _ := workspace.Resolve(root)
	ctx, _ := store.New(stateRoot).ContextFor(ws)
	raw, err := os.ReadFile(cfhome.ConfigPath(ctx.CFHome))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "second") {
		t.Fatalf("forced import did not replace target: %s", raw)
	}
}

func TestImportReportsMissingGlobalTarget(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	t.Setenv("HOME", t.TempDir())

	result := runFromDirectory(t, markerWorkspace(t), []string{"cfs", "import", "--yes"})
	if result.code != exitUnavailable || !strings.Contains(result.stderr, "no active global CF target") {
		t.Fatalf("result = %#v", result)
	}
}

func TestImportRejectsGlobalConfigurationWithoutAPITarget(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte("{\"Target\":\"\"}"))

	result := runFromDirectory(t, markerWorkspace(t), []string{"cfs", "import", "--yes"})
	if result.code != exitUnavailable || !strings.Contains(result.stderr, "no active global CF target") {
		t.Fatalf("result = %#v", result)
	}
}

func TestImportOutsideWorkspaceIncludesResolutionHint(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"global"}`))

	result := runFromDirectory(t, t.TempDir(), []string{"cfs", "import", "--yes"})
	if result.code != exitUnavailable || !strings.Contains(result.stderr, "CFS_WORKSPACE_ROOT") {
		t.Fatalf("result = %#v", result)
	}
}

func TestImportRejectsExternalCFHome(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	t.Setenv("CF_HOME", t.TempDir())

	result := runFromDirectory(t, markerWorkspace(t), []string{"cfs", "import", "--yes"})
	if result.code != exitUsage || !strings.Contains(result.stderr, "unset CF_HOME") {
		t.Fatalf("result = %#v", result)
	}
}

func TestImportRejectsConcurrentWorkspaceCommand(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"global"}`))
	root := markerWorkspace(t)
	ws, err := workspace.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := store.New(stateRoot).ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(stateRoot).Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(ctx.LockPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	result := runFromDirectory(t, root, []string{"cfs", "import", "--yes"})
	if result.code != exitTemporary || !strings.Contains(result.stderr, "active CF command") {
		t.Fatalf("result = %#v", result)
	}
}

func TestShimSuggestsImportForFreshWorkspace(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"global"}`))
	root := markerWorkspace(t)

	result := runFromDirectory(t, root, []string{"cf", "apps"})
	if result.code == exitOK || !strings.Contains(result.stderr, "run 'cfs import'") {
		t.Fatalf("result = %#v", result)
	}

	imported := runFromDirectory(t, root, []string{"cfs", "import", "--yes"})
	if imported.code != exitOK {
		t.Fatalf("import after hint = %#v", imported)
	}
}

func TestSuccessfulShimCommandDoesNotProbeGlobalTarget(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"global"}`))
	callLog := filepath.Join(t.TempDir(), "calls")
	t.Setenv("CFS_TEST_CALL_LOG", callLog)

	result := runFromDirectory(t, markerWorkspace(t), []string{"cf", "version"})
	if result.code != exitOK {
		t.Fatalf("result = %#v", result)
	}
	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "version\n" {
		t.Fatalf("official CF invocations = %q, want only version", got)
	}
}

func TestDoctorSuggestsImportForFreshWorkspace(t *testing.T) {
	fakeCF := writeTargetAwareFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	writeCFConfig(t, globalHome, []byte(`{"Target":"global"}`))
	root := markerWorkspace(t)
	ws, err := workspace.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	check := inspectWorkspaceTarget(cfg, ws)
	if check.Status != checkWarn || !strings.Contains(check.Message, "cfs import") {
		t.Fatalf("check = %#v", check)
	}
}

func writeTargetAwareFakeCF(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cf-real")
	script := `#!/bin/sh
config=$CF_HOME/.cf/config.json
if [ -n "$CFS_TEST_CALL_LOG" ]; then
  printf '%s\n' "$1" >>"$CFS_TEST_CALL_LOG"
fi
if [ "$1" = "target" ]; then
  [ -s "$config" ] && grep -q '"Target"' "$config"
  exit $?
fi
if [ "$1" = "apps" ]; then
  if [ ! -s "$config" ]; then
    mkdir -p "$(dirname "$config")"
    printf '{}\n' >"$config"
    chmod 600 "$config"
    exit 1
  fi
fi
printf 'CF_HOME=%s\n' "$CF_HOME"
printf 'ARGS=%s\n' "$*"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCFConfig(t *testing.T, home string, content []byte) {
	t.Helper()
	path := cfhome.ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
