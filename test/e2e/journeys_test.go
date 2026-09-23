//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallAndUninstallJourney(t *testing.T) {
	env := newInstalledTestEnvironment(t)
	project := markerWorkspace(t, env.root, "install-journey")

	beforeSetup := env.run(t, "cf", project, nil, "version")
	requireSuccess(t, "official CLI before setup", beforeSetup)
	assertPathResolvesTo(t, "cf", env.realCF)

	setupCFS(t, env)
	assertPathResolvesTo(t, "cf", filepath.Join(env.shimDir, executableName("cf")))

	freshShell := env.run(t, "sh", project, nil, "-c", "command -v cf && cf version")
	requireSuccess(t, "fresh shell after setup", freshShell)
	assertContains(t, freshShell.stdout, env.shimDir, "fresh shell did not resolve the cfs shim")

	doctor := env.run(t, env.cfs, project, nil, "doctor", "--json")
	requireSuccess(t, "doctor after setup", doctor)
	assertDoctorHasNoFailure(t, doctor.stdout)
	assertControlCommandsAvailable(t, env)

	repeatedSetup := env.run(t, env.cfs, env.repoRoot, nil, "setup", "--json")
	requireSuccess(t, "idempotent setup", repeatedSetup)
	assertJSONField(t, repeatedSetup.stdout, "path_ready", true)

	stateBeforeUninstall := directorySnapshot(t, env.state)
	uninstall := env.run(t, env.cfs, env.repoRoot, nil, "uninstall")
	requireSuccess(t, "cfs uninstall", uninstall)
	if _, err := os.Lstat(filepath.Join(env.shimDir, executableName("cf"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shim still exists after uninstall: %v", err)
	}
	assertEqualStrings(t, directorySnapshot(t, env.state), stateBeforeUninstall, "uninstall changed workspace state")

	assertPathResolvesTo(t, "cf", env.realCF)
	afterUninstall := env.run(t, "sh", project, nil, "-c", "command -v cf && cf version")
	requireSuccess(t, "fresh shell after uninstall", afterUninstall)
	assertContains(t, afterUninstall.stdout, filepath.Dir(env.realCF), "uninstall did not restore the official CLI")
}

func TestParallelWorkspaceSessions(t *testing.T) {
	mock := newMockCF()
	t.Cleanup(mock.close)
	env := newInstalledTestEnvironment(t)
	outside := makeDirectory(t, filepath.Join(env.root, "outside"))

	loginToTarget(t, env, mock, env.realCF, outside, globalMockTarget)
	globalConfig := filepath.Join(env.home, ".cf", "config.json")
	globalBefore := rawFileDigest(t, globalConfig)
	setupCFS(t, env)

	sessions := make([]workspaceSession, 0, len(workspaceMockTargets))
	loginCommands := make([]processCommand, 0, len(workspaceMockTargets))
	for _, target := range workspaceMockTargets {
		workspace := markerWorkspace(t, env.root, target.name)
		directory := workspace
		if target.name == "orders" {
			directory = makeDirectory(t, filepath.Join(workspace, "services", "api"))
		}
		arguments, input := target.loginCommand(mock.server.URL)
		sessions = append(sessions, workspaceSession{target: target, directory: directory})
		loginCommands = append(loginCommands, processCommand{
			name: target.name, executable: "cf", directory: directory, input: input, arguments: arguments,
		})
	}

	mock.expectConcurrentTokenRequests(len(loginCommands))
	loginResults := runConcurrentCommands(env, loginCommands)
	assertConcurrentResults(t, "login", loginCommands, loginResults)
	if t.Failed() {
		return
	}
	if got := mock.tokenRequestCount(); got != len(loginCommands) {
		t.Fatalf("concurrent login requests = %d, want %d", got, len(loginCommands))
	}

	mock.expectConcurrentAppRequests(len(sessions))
	appCommands := make([]processCommand, 0, len(sessions))
	for _, session := range sessions {
		appCommands = append(appCommands, processCommand{
			name: session.target.name, executable: "cf", directory: session.directory, arguments: []string{"apps", "--no-stats"},
		})
	}
	appResults := runConcurrentCommands(env, appCommands)
	for _, session := range sessions {
		session := session
		t.Run("apps/"+session.target.name, func(t *testing.T) {
			result := appResults[session.target.name]
			requireSuccess(t, "apps in workspace "+session.target.name, result)
			assertContains(t, result.stdout, session.target.appName, "workspace returned the wrong app")
			for _, other := range workspaceMockTargets {
				if other.name != session.target.name && strings.Contains(result.stdout, other.appName) {
					t.Fatalf("workspace %s exposed app from workspace %s", session.target.name, other.name)
				}
			}
		})
	}
	if t.Failed() {
		return
	}
	if got := mock.appRequestCount(); got != len(appCommands) {
		t.Fatalf("concurrent app requests = %d, want %d", got, len(appCommands))
	}

	contexts := make(map[string]string, len(sessions))
	for _, session := range sessions {
		status := env.run(t, env.cfs, session.directory, nil, "status", "--json", "--redact")
		requireSuccess(t, "status in fresh process for "+session.target.name, status)
		contextID := contextFromStatus(t, status.stdout)
		if owner, exists := contexts[contextID]; exists {
			t.Fatalf("workspaces %s and %s shared context %s", owner, session.target.name, contextID)
		}
		contexts[contextID] = session.target.name

		target := env.run(t, "cf", session.directory, nil, "target")
		requireSuccess(t, "target in fresh process for "+session.target.name, target)
		assertContains(t, target.stdout, session.target.orgName, "fresh process lost workspace organization")
		assertContains(t, target.stdout, session.target.spaceName, "fresh process lost workspace space")
	}

	assertRawDigest(t, globalConfig, globalBefore, "parallel workspace sessions changed global CF configuration")
	assertMetadataHasNoSecrets(t, env.state, syntheticSecrets())
}

func TestSameWorkspaceSessionsShareAndCoordinate(t *testing.T) {
	mock := newMockCF()
	t.Cleanup(mock.close)
	env := newInstalledTestEnvironment(t)
	setupCFS(t, env)

	workspace := markerWorkspace(t, env.root, "shared-workspace")
	target := workspaceMockTargets[0]
	loginToTarget(t, env, mock, "cf", workspace, target)

	first := env.run(t, "cf", workspace, nil, "target")
	second := env.run(t, "cf", workspace, nil, "target")
	requireSuccess(t, "first terminal target", first)
	requireSuccess(t, "second terminal target", second)
	assertContains(t, first.stdout, target.spaceName, "first terminal lost the shared target")
	assertContains(t, second.stdout, target.spaceName, "second terminal lost the shared target")

	assertSameWorkspaceLock(t, env, mock, workspace)
	afterRelease := env.run(t, "cf", workspace, nil, "target")
	requireSuccess(t, "same workspace after lock release", afterRelease)
}

func TestInterruptedSessionReleasesWorkspaceLock(t *testing.T) {
	mock := newMockCF()
	t.Cleanup(mock.close)
	env := newInstalledTestEnvironment(t)
	setupCFS(t, env)

	workspace := markerWorkspace(t, env.root, "interrupted-workspace")
	target := workspaceMockTargets[0]
	loginToTarget(t, env, mock, "cf", workspace, target)

	command := exec.Command("cf", "curl", "/e2e/block")
	command.Dir = workspace
	command.Env = env.env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start long-running session: %v", err)
	}
	t.Cleanup(func() {
		mock.unblock()
		if command.ProcessState == nil {
			_ = command.Process.Kill()
		}
	})

	select {
	case <-mock.blockStarted:
	case <-time.After(processStartupTimeout):
		t.Fatal("long-running session did not reach the mock API")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt long-running session: %v", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case <-wait:
	case <-time.After(processShutdownTimeout):
		t.Fatalf("interrupted session did not exit: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	afterInterrupt := env.run(t, "cf", workspace, nil, "target")
	requireSuccess(t, "workspace command after interrupted session", afterInterrupt)
}

func TestGitWorktreeJourney(t *testing.T) {
	mock := newMockCF()
	t.Cleanup(mock.close)
	env := newInstalledTestEnvironment(t)
	setupCFS(t, env)

	mainWorktree, linkedWorktree := createGitWorktrees(t, env)
	mainTarget := workspaceMockTargets[0]
	linkedTarget := workspaceMockTargets[1]
	loginToTarget(t, env, mock, "cf", mainWorktree, mainTarget)
	loginToTarget(t, env, mock, "cf", linkedWorktree, linkedTarget)

	assertWorkspaceApp(t, env, mainWorktree, mainTarget)
	assertWorkspaceApp(t, env, linkedWorktree, linkedTarget)
	mainStatus := env.run(t, env.cfs, mainWorktree, nil, "status", "--json", "--redact")
	linkedStatus := env.run(t, env.cfs, linkedWorktree, nil, "status", "--json", "--redact")
	requireSuccess(t, "main worktree status", mainStatus)
	requireSuccess(t, "linked worktree status", linkedStatus)
	assertJSONField(t, mainStatus.stdout, "source", "git-worktree")
	assertJSONField(t, linkedStatus.stdout, "source", "git-worktree")
	if contextFromStatus(t, mainStatus.stdout) == contextFromStatus(t, linkedStatus.stdout) {
		t.Fatal("two Git worktrees shared one cfs context")
	}
}

func TestContextLifecycleJourney(t *testing.T) {
	mock := newMockCF()
	t.Cleanup(mock.close)
	env := newInstalledTestEnvironment(t)
	outside := makeDirectory(t, filepath.Join(env.root, "outside"))
	importProject := markerWorkspace(t, env.root, "imported")
	activeProject := markerWorkspace(t, env.root, "active")

	loginToTarget(t, env, mock, env.realCF, outside, globalMockTarget)
	globalConfig := filepath.Join(env.home, ".cf", "config.json")
	requireSuccess(t, "global target before lifecycle", env.run(t, env.realCF, outside, nil, "target"))
	globalBefore := configSnapshot(t, globalConfig)
	globalRawBefore := rawFileDigest(t, globalConfig)
	setupCFS(t, env)
	assertRawDigest(t, globalConfig, globalRawBefore, "setup changed global CF configuration")

	bypassApps := env.run(t, "cf", outside, map[string]string{"CFS_DISABLE": "1"}, "apps", "--no-stats")
	requireSuccess(t, "explicit isolation bypass", bypassApps)
	assertContains(t, bypassApps.stdout, globalMockTarget.appName, "isolation bypass did not use the global target")
	assertRawDigest(t, globalConfig, globalRawBefore, "isolation bypass changed global CF configuration")

	outsideResult := env.run(t, "cf", outside, nil, "target")
	if outsideResult.code != exitUnavailable || !strings.Contains(outsideResult.stderr, "no workspace could be resolved") {
		t.Fatalf("outside-workspace command did not fail closed: code=%d", outsideResult.code)
	}
	assertRawDigest(t, globalConfig, globalRawBefore, "outside-workspace command changed global CF configuration")

	imported := env.run(t, env.cfs, importProject, nil, "import", "--yes")
	requireSuccess(t, "cfs import", imported)
	assertWorkspaceApp(t, env, importProject, globalMockTarget)

	loginToTarget(t, env, mock, "cf", activeProject, workspaceMockTargets[0])
	rejectedImport := env.run(t, env.cfs, activeProject, nil, "import", "--yes")
	if rejectedImport.code != exitUsage || !strings.Contains(rejectedImport.stderr, "use --force") {
		t.Fatalf("import replaced an active context without --force: code=%d", rejectedImport.code)
	}
	forcedImport := env.run(t, env.cfs, activeProject, nil, "import", "--yes", "--force")
	requireSuccess(t, "forced import over active context", forcedImport)
	assertWorkspaceApp(t, env, activeProject, globalMockTarget)

	status := env.run(t, env.cfs, activeProject, nil, "status", "--json", "--redact")
	requireSuccess(t, "redacted status", status)
	assertRedactedStatus(t, status.stdout, []string{env.root, mock.server.URL, globalMockTarget.orgName, globalMockTarget.spaceName})

	pluginHome := makeDirectory(t, filepath.Join(env.root, "explicit-plugin-home"))
	plugins := env.run(t, "cf", activeProject, map[string]string{"CF_PLUGIN_HOME": pluginHome}, "plugins")
	requireSuccess(t, "explicit plugin home", plugins)
	failedCommand := env.run(t, "cf", activeProject, nil, "not-a-real-command")
	if failedCommand.code != 1 {
		t.Fatalf("official CLI failure exit code=%d, want 1", failedCommand.code)
	}

	assertExplicitWorkspace(t, env, outside)
	assertExternalCFHome(t, env, mock, outside, workspaceMockTargets[1])
	assertReset(t, env, importProject)
	assertGarbageCollection(t, env)
	assertMetadataHasNoSecrets(t, env.state, syntheticSecrets())
	assertRawDigest(t, globalConfig, globalRawBefore, "workspace lifecycle changed global CF configuration")
	assertConfigSnapshot(t, globalConfig, globalBefore, "workspace lifecycle changed global CF state")
}
