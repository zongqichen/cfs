//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	exitUsage       = 64
	exitUnavailable = 69
	exitTemporary   = 75
)

type commandResult struct {
	stdout string
	stderr string
	code   int
}

type testEnvironment struct {
	repoRoot string
	root     string
	home     string
	state    string
	shimDir  string
	realCF   string
	cfs      string
	env      []string
}

func TestRealCFWorkspaceLifecycle(t *testing.T) {
	mock := newMockCF()
	defer mock.close()

	testEnv := newTestEnvironment(t)
	projectA := markerWorkspace(t, testEnv.root, "orders")
	projectASubdirectory := makeDirectory(t, filepath.Join(projectA, "services", "api"))
	projectB := markerWorkspace(t, testEnv.root, "payments")
	importProject := markerWorkspace(t, testEnv.root, "imported")
	outsideWorkspace := makeDirectory(t, filepath.Join(testEnv.root, "outside"))

	globalLogin := testEnv.run(t, testEnv.realCF, outsideWorkspace, nil, "login",
		"-a", mock.server.URL, "--skip-ssl-validation",
		"-u", testUsername, "-p", testPassword,
		"-o", "global-org", "-s", "global-space")
	requireSuccess(t, "global login with the official CLI", globalLogin)
	globalConfig := filepath.Join(testEnv.home, ".cf", "config.json")
	requireSuccess(t, "global target with the official CLI", testEnv.run(t, testEnv.realCF, outsideWorkspace, nil, "target"))
	globalBefore := configSnapshot(t, globalConfig)
	globalRawBefore := rawFileDigest(t, globalConfig)

	setup := testEnv.run(t, testEnv.cfs, testEnv.repoRoot, nil, "setup",
		"--real-cf", testEnv.realCF, "--shim-dir", testEnv.shimDir, "--state-dir", testEnv.state, "--json")
	requireSuccess(t, "cfs setup", setup)
	assertJSONField(t, setup.stdout, "path_ready", true)
	assertRawDigest(t, globalConfig, globalRawBefore, "setup changed global CF configuration")
	requireSuccess(t, "cfs help", testEnv.run(t, testEnv.cfs, testEnv.repoRoot, nil, "help"))
	requireSuccess(t, "cfs version", testEnv.run(t, testEnv.cfs, testEnv.repoRoot, nil, "version"))
	bypassApps := testEnv.run(t, "cf", outsideWorkspace, map[string]string{"CFS_DISABLE": "1"}, "apps", "--no-stats")
	requireSuccess(t, "explicit isolation bypass", bypassApps)
	assertContains(t, bypassApps.stdout, "global-app", "isolation bypass did not use the global target")
	assertRawDigest(t, globalConfig, globalRawBefore, "isolation bypass changed global CF configuration")

	doctor := testEnv.run(t, testEnv.cfs, projectA, nil, "doctor", "--json")
	requireSuccess(t, "cfs doctor", doctor)
	assertDoctorHasNoFailure(t, doctor.stdout)
	assertRawDigest(t, globalConfig, globalRawBefore, "doctor changed global CF configuration")

	outside := testEnv.run(t, "cf", outsideWorkspace, nil, "target")
	if outside.code != exitUnavailable || !strings.Contains(outside.stderr, "no workspace could be resolved") {
		t.Fatalf("outside-workspace command did not fail closed: code=%d", outside.code)
	}
	assertRawDigest(t, globalConfig, globalRawBefore, "outside-workspace command changed global CF configuration")

	imported := testEnv.run(t, testEnv.cfs, importProject, nil, "import", "--yes")
	requireSuccess(t, "cfs import", imported)
	importedApps := testEnv.run(t, "cf", importProject, nil, "apps", "--no-stats")
	requireSuccess(t, "apps after importing the global context", importedApps)
	assertContains(t, importedApps.stdout, "global-app", "imported context did not use the global target")

	loginA := testEnv.runWithInput(t, "cf", projectA, nil, testPassword+"\n", "login",
		"-a", mock.server.URL, "--skip-ssl-validation",
		"-u", testUsername,
		"-o", "commerce", "-s", "development")
	requireSuccess(t, "workspace A login", loginA)

	loginB := testEnv.run(t, "cf", projectB, nil, "login",
		"-a", mock.server.URL, "--skip-ssl-validation",
		"--sso-passcode", testPasscode,
		"-o", "finance", "-s", "production")
	requireSuccess(t, "workspace B SSO login", loginB)
	assertRawDigest(t, globalConfig, globalRawBefore, "workspace logins changed global CF configuration")

	appsA := testEnv.run(t, "cf", projectASubdirectory, nil, "apps", "--no-stats")
	requireSuccess(t, "workspace A apps", appsA)
	assertContains(t, appsA.stdout, "orders-app", "workspace A used the wrong space")
	if strings.Contains(appsA.stdout, "payments-app") {
		t.Fatal("workspace A exposed workspace B data")
	}

	appsB := testEnv.run(t, "cf", projectB, nil, "apps", "--no-stats")
	requireSuccess(t, "workspace B apps", appsB)
	assertContains(t, appsB.stdout, "payments-app", "workspace B used the wrong space")
	if !mock.sawAppRequest("space-development") || !mock.sawAppRequest("space-production") {
		t.Fatal("the official CLI did not query both isolated space GUIDs")
	}

	reused := testEnv.run(t, "cf", projectASubdirectory, nil, "target")
	requireSuccess(t, "fresh process reusing workspace A", reused)
	assertContains(t, reused.stdout, "commerce", "fresh process lost workspace A org")
	assertContains(t, reused.stdout, "development", "fresh process lost workspace A space")

	status := testEnv.run(t, testEnv.cfs, projectA, nil, "status", "--json", "--redact")
	requireSuccess(t, "redacted status", status)
	assertRedactedStatus(t, status.stdout, []string{testEnv.root, mock.server.URL, "commerce", "development"})

	pluginHome := makeDirectory(t, filepath.Join(testEnv.root, "explicit-plugin-home"))
	plugins := testEnv.run(t, "cf", projectA, map[string]string{"CF_PLUGIN_HOME": pluginHome}, "plugins")
	requireSuccess(t, "official plugin discovery through the shim", plugins)

	failedCommand := testEnv.run(t, "cf", projectA, nil, "not-a-real-command")
	if failedCommand.code != 1 {
		t.Fatalf("official CLI failure exit code=%d, want 1", failedCommand.code)
	}

	assertParallelWorkspaces(t, testEnv, mock, projectA, projectB)
	assertSameWorkspaceLock(t, testEnv, mock, projectA)
	assertGitWorktreeIsolation(t, testEnv)
	assertExplicitWorkspace(t, testEnv, outsideWorkspace)
	assertExternalCFHome(t, testEnv, mock, outsideWorkspace)
	assertReset(t, testEnv, importProject)
	assertGarbageCollection(t, testEnv)
	assertMetadataHasNoSecrets(t, testEnv.state)

	rejectedImport := testEnv.run(t, testEnv.cfs, projectA, nil, "import", "--yes")
	if rejectedImport.code != exitUsage || !strings.Contains(rejectedImport.stderr, "use --force") {
		t.Fatalf("import replaced an active context without --force: code=%d", rejectedImport.code)
	}
	forcedImport := testEnv.run(t, testEnv.cfs, projectA, nil, "import", "--yes", "--force")
	requireSuccess(t, "forced import over an active context", forcedImport)
	forcedApps := testEnv.run(t, "cf", projectA, nil, "apps", "--no-stats")
	requireSuccess(t, "apps after forced import", forcedApps)
	assertContains(t, forcedApps.stdout, "global-app", "forced import did not replace the workspace target")

	stateBeforeUninstall := directorySnapshot(t, testEnv.state)
	uninstall := testEnv.run(t, testEnv.cfs, testEnv.repoRoot, nil, "uninstall")
	requireSuccess(t, "cfs uninstall", uninstall)
	if _, err := os.Lstat(filepath.Join(testEnv.shimDir, executableName("cf"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shim still exists after uninstall: %v", err)
	}
	if got := directorySnapshot(t, testEnv.state); !equalStrings(got, stateBeforeUninstall) {
		t.Fatal("uninstall changed workspace state")
	}
	assertRawDigest(t, globalConfig, globalRawBefore, "cfs lifecycle changed global CF configuration")

	directCF, err := exec.LookPath("cf")
	if err != nil {
		t.Fatalf("find cf after uninstall: %v", err)
	}
	if sameFile(directCF, testEnv.cfs) {
		t.Fatalf("cf still resolves to cfs after uninstall: %s", directCF)
	}
	directTarget := testEnv.run(t, "cf", outsideWorkspace, nil, "target")
	requireSuccess(t, "direct official CLI after uninstall", directTarget)
	assertContains(t, directTarget.stdout, "global-org", "uninstall did not restore the global target")
	assertConfigSnapshot(t, globalConfig, globalBefore, "lifecycle changed global CF state")
}

func newTestEnvironment(t *testing.T) testEnvironment {
	t.Helper()
	realCF := os.Getenv("CFS_REAL_CF")
	if realCF == "" {
		t.Fatal("CFS_REAL_CF must point to an official cf CLI binary")
	}
	resolvedCF, err := filepath.EvalSymlinks(realCF)
	if err != nil {
		t.Fatalf("resolve CFS_REAL_CF: %v", err)
	}
	resolvedCF, err = filepath.Abs(resolvedCF)
	if err != nil {
		t.Fatalf("make CFS_REAL_CF absolute: %v", err)
	}
	if filepath.Base(resolvedCF) == "cfs" {
		t.Fatal("CFS_REAL_CF resolves to the cfs shim; provide the official CLI binary")
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve E2E source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	root := t.TempDir()
	binDir := makeDirectory(t, filepath.Join(root, "bin"))
	officialDir := makeDirectory(t, filepath.Join(root, "official-bin"))
	shimDir := filepath.Join(root, "shims")
	home := makeDirectory(t, filepath.Join(root, "home"))
	state := filepath.Join(root, "state")
	temporary := makeDirectory(t, filepath.Join(root, "tmp"))

	officialCF := filepath.Join(officialDir, executableName("cf"))
	if err := copyFile(resolvedCF, officialCF); err != nil {
		t.Fatalf("copy official cf: %v", err)
	}

	install := exec.Command("go", "install", "./cmd/cfs")
	install.Dir = repoRoot
	install.Env = replaceEnvironment(os.Environ(), map[string]string{"GOBIN": binDir})
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install cfs into E2E environment: %v: %s", err, output)
	}

	testPath := strings.Join([]string{shimDir, officialDir, binDir, os.Getenv("PATH")}, string(os.PathListSeparator))
	env := replaceEnvironment(os.Environ(), map[string]string{
		"HOME":                home,
		"USERPROFILE":         home,
		"PATH":                testPath,
		"TMPDIR":              temporary,
		"TEMP":                temporary,
		"TMP":                 temporary,
		"APPDATA":             filepath.Join(root, "appdata", "roaming"),
		"LOCALAPPDATA":        filepath.Join(root, "appdata", "local"),
		"XDG_CONFIG_HOME":     filepath.Join(root, "xdg-config"),
		"XDG_STATE_HOME":      filepath.Join(root, "xdg-state"),
		"GIT_CONFIG_GLOBAL":   filepath.Join(root, "gitconfig"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"CFS_CONFIG_FILE":     filepath.Join(root, "config", "config.json"),
		"CFS_STATE_HOME":      state,
		"CFS_SHIM_DIR":        shimDir,
		"CF_COLOR":            "false",
		"LANG":                "C",
		"LC_ALL":              "C",
	})
	env = removeEnvironment(env, "CF_HOME", "CF_PLUGIN_HOME", "CF_TRACE", "CFS_ACTIVE_CONTEXT", "CFS_DISABLE", "CFS_LOCK_TIMEOUT", "CFS_WORKSPACE_ROOT")
	t.Setenv("PATH", testPath)

	return testEnvironment{
		repoRoot: repoRoot, root: root, home: home, state: state, shimDir: shimDir,
		realCF: officialCF, cfs: filepath.Join(binDir, executableName("cfs")), env: env,
	}
}

func (e testEnvironment) run(t *testing.T, executable, directory string, overrides map[string]string, args ...string) commandResult {
	t.Helper()
	return e.runWithInput(t, executable, directory, overrides, "", args...)
}

func (e testEnvironment) runWithInput(t *testing.T, executable, directory string, overrides map[string]string, input string, args ...string) commandResult {
	t.Helper()
	result := runCommandWithInput(executable, directory, replaceEnvironment(e.env, overrides), input, args...)
	if result.code == -1 {
		t.Fatalf("start command %s: %s", filepath.Base(executable), result.stderr)
	}
	return result
}

func runCommand(executable, directory string, env []string, args ...string) commandResult {
	return runCommandWithInput(executable, directory, env, "", args...)
}

func runCommandWithInput(executable, directory string, env []string, input string, args ...string) commandResult {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = env
	command.Stdin = strings.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.code = exitError.ExitCode()
		return result
	}
	result.code = -1
	if ctx.Err() != nil {
		result.stderr = "command timed out"
	} else {
		result.stderr = err.Error()
	}
	return result
}

func assertParallelWorkspaces(t *testing.T, env testEnvironment, mock *mockCF, projectA, projectB string) {
	t.Helper()
	results := make(chan commandResult, 2)
	go func() {
		results <- runCommand("cf", projectA, env.env, "curl", "/e2e/barrier?workspace=orders")
	}()
	go func() {
		results <- runCommand("cf", projectB, env.env, "curl", "/e2e/barrier?workspace=payments")
	}()
	for range 2 {
		requireSuccess(t, "parallel command in a distinct workspace", <-results)
	}
	if arrivals := mock.barrierArrivalCount(); arrivals != 2 {
		t.Fatalf("parallel API barrier saw %d commands, want 2", arrivals)
	}
}

func assertSameWorkspaceLock(t *testing.T, env testEnvironment, mock *mockCF, project string) {
	t.Helper()
	defer mock.unblock()
	firstResult := make(chan commandResult, 1)
	go func() {
		firstResult <- runCommand("cf", project, env.env, "curl", "/e2e/block")
	}()

	select {
	case <-mock.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first same-workspace command did not reach the mock API")
	}
	second := env.run(t, "cf", project, map[string]string{"CFS_LOCK_TIMEOUT": "100ms"}, "target")
	if second.code != exitTemporary || !strings.Contains(second.stderr, "already has an active CF command") {
		t.Fatalf("second same-workspace command was not rejected: code=%d", second.code)
	}
	mock.unblock()
	requireSuccess(t, "first same-workspace command after release", <-firstResult)
}

func assertGitWorktreeIsolation(t *testing.T, env testEnvironment) {
	t.Helper()
	mainWorktree := makeDirectory(t, filepath.Join(env.root, "repository", "main"))
	linkedWorktree := filepath.Join(env.root, "repository", "linked")
	requireSuccess(t, "git init", env.run(t, "git", mainWorktree, nil, "init", "-b", "main"))
	if err := os.WriteFile(filepath.Join(mainWorktree, "README.md"), []byte("e2e\n"), 0o600); err != nil {
		t.Fatalf("write worktree fixture: %v", err)
	}
	requireSuccess(t, "git add", env.run(t, "git", mainWorktree, nil, "add", "README.md"))
	requireSuccess(t, "git commit", env.run(t, "git", mainWorktree, nil,
		"-c", "user.name=cfs e2e", "-c", "user.email=cfs-e2e@example.invalid", "commit", "-m", "fixture"))
	requireSuccess(t, "git worktree add", env.run(t, "git", mainWorktree, nil, "worktree", "add", "-b", "linked", linkedWorktree))

	requireSuccess(t, "main worktree command", env.run(t, "cf", mainWorktree, nil, "config", "--color", "false"))
	requireSuccess(t, "linked worktree command", env.run(t, "cf", linkedWorktree, nil, "config", "--color", "true"))
	mainStatus := env.run(t, env.cfs, mainWorktree, nil, "status", "--json", "--redact")
	linkedStatus := env.run(t, env.cfs, linkedWorktree, nil, "status", "--json", "--redact")
	assertJSONField(t, mainStatus.stdout, "source", "git-worktree")
	assertJSONField(t, linkedStatus.stdout, "source", "git-worktree")
	if contextFromStatus(t, mainStatus.stdout) == contextFromStatus(t, linkedStatus.stdout) {
		t.Fatal("two Git worktrees shared one cfs context")
	}
}

func assertExplicitWorkspace(t *testing.T, env testEnvironment, outside string) {
	t.Helper()
	explicitRoot := makeDirectory(t, filepath.Join(env.root, "explicit-root"))
	overrides := map[string]string{"CFS_WORKSPACE_ROOT": explicitRoot}
	requireSuccess(t, "explicit workspace command", env.run(t, "cf", outside, overrides, "config", "--color", "false"))
	status := env.run(t, env.cfs, outside, overrides, "status", "--json", "--redact")
	requireSuccess(t, "explicit workspace status", status)
	assertJSONField(t, status.stdout, "source", "environment")
	doctor := env.run(t, env.cfs, outside, overrides, "doctor", "--json")
	requireSuccess(t, "explicit workspace doctor", doctor)
	assertDoctorCheck(t, doctor.stdout, "workspace-target", "warn")
}

func assertExternalCFHome(t *testing.T, env testEnvironment, mock *mockCF, outside string) {
	t.Helper()
	externalHome := makeDirectory(t, filepath.Join(env.root, "external-cf-home"))
	overrides := map[string]string{"CF_HOME": externalHome}
	login := env.run(t, env.realCF, outside, overrides, "login",
		"-a", mock.server.URL, "--skip-ssl-validation",
		"-u", testUsername, "-p", testPassword, "-o", "finance", "-s", "production")
	requireSuccess(t, "login with explicit CF_HOME", login)
	apps := env.run(t, filepath.Join(env.shimDir, executableName("cf")), outside, overrides, "apps", "--no-stats")
	requireSuccess(t, "shim with explicit CF_HOME", apps)
	assertContains(t, apps.stdout, "payments-app", "explicit CF_HOME was not preserved")
}

func assertReset(t *testing.T, env testEnvironment, project string) {
	t.Helper()
	reset := env.run(t, env.cfs, project, nil, "reset", "--yes")
	requireSuccess(t, "workspace reset", reset)
	assertContains(t, reset.stdout, "Moved workspace state", "reset did not move state to trash")
	doctor := env.run(t, env.cfs, project, nil, "doctor", "--json")
	requireSuccess(t, "doctor after reset", doctor)
	assertDoctorCheck(t, doctor.stdout, "workspace-target", "warn")
}

func assertGarbageCollection(t *testing.T, env testEnvironment) {
	t.Helper()
	orphan := markerWorkspace(t, env.root, "orphan")
	requireSuccess(t, "create orphan candidate", env.run(t, "cf", orphan, nil, "config", "--color", "false"))
	if err := os.Rename(orphan, orphan+"-removed"); err != nil {
		t.Fatalf("detach orphan workspace: %v", err)
	}

	dryRun := env.run(t, env.cfs, env.repoRoot, nil, "gc", "--json")
	requireSuccess(t, "garbage collection dry run", dryRun)
	assertGCAction(t, dryRun.stdout, "would-trash")
	apply := env.run(t, env.cfs, env.repoRoot, nil, "gc", "--apply", "--json")
	requireSuccess(t, "garbage collection apply", apply)
	assertGCAction(t, apply.stdout, "trashed")
}

func markerWorkspace(t *testing.T, root, name string) string {
	t.Helper()
	workspace := makeDirectory(t, filepath.Join(root, "workspaces", name))
	if err := os.WriteFile(filepath.Join(workspace, ".cfs.toml"), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatalf("write workspace marker: %v", err)
	}
	return workspace
}

func makeDirectory(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
	return path
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func replaceEnvironment(base []string, replacements map[string]string) []string {
	if len(replacements) == 0 {
		return append([]string(nil), base...)
	}
	keys := mapKeys(replacements)
	result := removeEnvironment(base, keys...)
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+replacements[key])
	}
	return result
}

func removeEnvironment(base []string, names ...string) []string {
	removed := make(map[string]struct{}, len(names))
	for _, name := range names {
		removed[normalizeEnvironmentName(name)] = struct{}{}
	}
	result := make([]string, 0, len(base))
	for _, entry := range base {
		name := normalizeEnvironmentName(strings.SplitN(entry, "=", 2)[0])
		if _, found := removed[name]; !found {
			result = append(result, entry)
		}
	}
	return result
}

func normalizeEnvironmentName(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func requireSuccess(t *testing.T, operation string, result commandResult) {
	t.Helper()
	if result.code != 0 {
		t.Fatalf("%s failed: code=%d stdout=%q stderr=%q", operation, result.code, result.stdout, result.stderr)
	}
}

func assertContains(t *testing.T, value, substring, message string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("%s: output=%q", message, value)
	}
}

func configSnapshot(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test configuration: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse test configuration: %v", err)
	}
	keys := []string{
		"AccessToken", "APIVersion", "AuthorizationEndpoint", "OrganizationFields",
		"RefreshToken", "SpaceFields", "SSLDisabled", "Target", "UaaEndpoint",
		"UAAGrantType", "UAAOAuthClient", "UAAOAuthClientSecret",
	}
	snapshot := make(map[string]string, len(keys))
	for _, key := range keys {
		value := document[key]
		canonical, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("canonicalize test configuration field: %v", err)
		}
		sum := sha256.Sum256(canonical)
		snapshot[key] = hex.EncodeToString(sum[:])
	}
	return snapshot
}

func rawFileDigest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test file for digest: %v", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func assertRawDigest(t *testing.T, path, expected, message string) {
	t.Helper()
	if actual := rawFileDigest(t, path); actual != expected {
		t.Fatal(message)
	}
}

func assertConfigSnapshot(t *testing.T, path string, expected map[string]string, message string) {
	t.Helper()
	actual := configSnapshot(t, path)
	var changed []string
	for key, expectedDigest := range expected {
		if actual[key] != expectedDigest {
			changed = append(changed, key)
		}
	}
	for key := range actual {
		if _, found := expected[key]; !found {
			changed = append(changed, key)
		}
	}
	if len(changed) > 0 {
		sort.Strings(changed)
		t.Fatalf("%s (changed fields: %s)", message, strings.Join(changed, ", "))
	}
}

func assertJSONField(t *testing.T, raw, field string, expected any) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse JSON output: %v", err)
	}
	if fmt.Sprint(value[field]) != fmt.Sprint(expected) {
		t.Fatalf("JSON field %s=%v, want %v", field, value[field], expected)
	}
}

func assertDoctorHasNoFailure(t *testing.T, raw string) {
	t.Helper()
	var value struct {
		Checks []struct {
			Name   string
			Status string
		}
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse doctor output: %v", err)
	}
	for _, check := range value.Checks {
		if check.Status == "fail" {
			t.Fatalf("doctor check failed: %s", check.Name)
		}
	}
}

func assertDoctorCheck(t *testing.T, raw, name, status string) {
	t.Helper()
	var value struct {
		Checks []struct {
			Name   string
			Status string
		}
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse doctor output: %v", err)
	}
	for _, check := range value.Checks {
		if check.Name == name {
			if check.Status != status {
				t.Fatalf("doctor check %s=%s, want %s", name, check.Status, status)
			}
			return
		}
	}
	t.Fatalf("doctor output is missing check %s", name)
}

func assertGCAction(t *testing.T, raw, action string) {
	t.Helper()
	var value struct {
		Contexts []struct {
			Action string
		}
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse gc output: %v", err)
	}
	for _, context := range value.Contexts {
		if context.Action == action {
			return
		}
	}
	t.Fatalf("gc output is missing action %s", action)
}

func assertRedactedStatus(t *testing.T, raw string, forbidden []string) {
	t.Helper()
	assertJSONField(t, raw, "redacted", true)
	for _, value := range forbidden {
		if strings.Contains(raw, value) {
			t.Fatal("redacted status exposed forbidden operational metadata")
		}
	}
}

func contextFromStatus(t *testing.T, raw string) string {
	t.Helper()
	var value struct {
		Context string
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse status output: %v", err)
	}
	if value.Context == "" {
		t.Fatal("status did not include a context ID")
	}
	return value.Context
}

func assertMetadataHasNoSecrets(t *testing.T, stateRoot string) {
	t.Helper()
	forbidden := []string{testPassword, testPasscode, "synthetic-refresh-token"}
	err := filepath.WalkDir(stateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "metadata.json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range forbidden {
			if bytes.Contains(raw, []byte(secret)) {
				return errors.New("cfs metadata contains synthetic credentials")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect cfs metadata: %v", err)
	}
}

func directorySnapshot(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot state directory: %v", err)
	}
	sort.Strings(paths)
	return paths
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameFile(first, second string) bool {
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}
