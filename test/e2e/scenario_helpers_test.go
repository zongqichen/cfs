//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type workspaceSession struct {
	target    mockTarget
	directory string
}

type namedContextSession struct {
	name   string
	target mockTarget
}

type processCommand struct {
	name       string
	executable string
	directory  string
	overrides  map[string]string
	input      string
	arguments  []string
}

type processResult struct {
	name   string
	result commandResult
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
	case <-time.After(processStartupTimeout):
		t.Fatal("first same-workspace command did not reach the mock API")
	}
	second := env.run(t, "cf", project, map[string]string{"CFS_LOCK_TIMEOUT": "100ms"}, "target")
	if second.code != exitTemporary || !strings.Contains(second.stderr, "already has an active CF command") {
		t.Fatalf("second same-workspace command was not rejected: code=%d", second.code)
	}
	mock.unblock()
	requireSuccess(t, "first same-workspace command after release", <-firstResult)
}

func assertNamedContextLocks(t *testing.T, env testEnvironment, mock *mockCF, project, busyName, independentName string) {
	t.Helper()
	defer mock.unblock()
	firstResult := make(chan commandResult, 1)
	go func() {
		firstResult <- runCommand(env.cfs, project, env.env, "-c", busyName, "curl", "/e2e/block")
	}()

	select {
	case <-mock.blockStarted:
	case <-time.After(processStartupTimeout):
		t.Fatal("first named-context command did not reach the mock API")
	}
	same := env.run(t, env.cfs, project, map[string]string{"CFS_LOCK_TIMEOUT": "100ms"}, "-c", busyName, "target")
	if same.code != exitTemporary || !strings.Contains(same.stderr, "active CF command") {
		t.Fatalf("second same-context command was not rejected: code=%d stdout=%q stderr=%q", same.code, same.stdout, same.stderr)
	}
	different := env.run(t, env.cfs, project, nil, "-c", independentName, "target")
	requireSuccess(t, "command in independently locked named context", different)

	mock.unblock()
	requireSuccess(t, "first named-context command after release", <-firstResult)
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

func assertExternalCFHome(t *testing.T, env testEnvironment, mock *mockCF, outside string, target mockTarget) {
	t.Helper()
	externalHome := makeDirectory(t, filepath.Join(env.root, "external-cf-home"))
	overrides := map[string]string{"CF_HOME": externalHome}
	arguments, input := target.loginCommand(mock.server.URL)
	login := env.runWithInput(t, env.realCF, outside, overrides, input, arguments...)
	requireSuccess(t, "login with explicit CF_HOME", login)
	apps := env.run(t, filepath.Join(env.shimDir, executableName("cf")), outside, overrides, "apps", "--no-stats")
	requireSuccess(t, "shim with explicit CF_HOME", apps)
	assertContains(t, apps.stdout, target.appName, "explicit CF_HOME was not preserved")
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

func setupCFS(t *testing.T, env testEnvironment) {
	t.Helper()
	setup := env.run(t, env.cfs, env.repoRoot, nil, "setup", "--json")
	requireSuccess(t, "cfs setup with automatic official CLI discovery", setup)
	assertJSONField(t, setup.stdout, "path_ready", true)
}

func assertControlCommandsAvailable(t *testing.T, env testEnvironment) {
	t.Helper()
	checks := []struct {
		name     string
		args     []string
		expected string
	}{
		{name: "help", args: []string{"help"}, expected: "cfs <command> [options]"},
		{name: "command help", args: []string{"help", "setup"}, expected: "cfs setup [options]"},
		{name: "version", args: []string{"version"}, expected: "cfs "},
	}
	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			result := env.run(t, env.cfs, env.repoRoot, nil, check.args...)
			requireSuccess(t, "cfs "+check.name, result)
			assertContains(t, result.stdout, check.expected, "unexpected cfs "+check.name+" output")
		})
	}
}

func loginToTarget(t *testing.T, env testEnvironment, mock *mockCF, executable, directory string, target mockTarget) {
	t.Helper()
	arguments, input := target.loginCommand(mock.server.URL)
	login := env.runWithInput(t, executable, directory, nil, input, arguments...)
	requireSuccess(t, "login to "+target.name, login)
}

func assertWorkspaceApp(t *testing.T, env testEnvironment, workspace string, target mockTarget) {
	t.Helper()
	apps := env.run(t, "cf", workspace, nil, "apps", "--no-stats")
	requireSuccess(t, "apps in workspace "+target.name, apps)
	assertContains(t, apps.stdout, target.appName, "workspace used the wrong target")
}

func assertNamedContextApp(t *testing.T, env testEnvironment, workspace, name string, target mockTarget) {
	t.Helper()
	apps := env.run(t, env.cfs, workspace, nil, "-c", name, "apps", "--no-stats")
	requireSuccess(t, "apps in named context "+name, apps)
	assertContains(t, apps.stdout, target.appName, "named context used the wrong target")
}

func createGitWorktrees(t *testing.T, env testEnvironment) (string, string) {
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
	return mainWorktree, linkedWorktree
}

func runConcurrentCommands(env testEnvironment, commands []processCommand) map[string]commandResult {
	start := make(chan struct{})
	results := make(chan processResult, len(commands))
	var ready sync.WaitGroup
	ready.Add(len(commands))

	for _, command := range commands {
		command := command
		go func() {
			ready.Done()
			<-start
			result := runCommandWithInput(
				command.executable,
				command.directory,
				replaceEnvironment(env.env, command.overrides),
				command.input,
				command.arguments...,
			)
			results <- processResult{name: command.name, result: result}
		}()
	}

	ready.Wait()
	close(start)
	collected := make(map[string]commandResult, len(commands))
	for range commands {
		result := <-results
		collected[result.name] = result.result
	}
	return collected
}

func assertConcurrentResults(t *testing.T, stage string, commands []processCommand, results map[string]commandResult) {
	t.Helper()
	for _, command := range commands {
		command := command
		t.Run(stage+"/"+command.name, func(t *testing.T) {
			result, found := results[command.name]
			if !found {
				t.Fatalf("%s result is missing", command.name)
			}
			requireSuccess(t, stage+" for workspace "+command.name, result)
		})
	}
}

func assertPathResolvesTo(t *testing.T, command, expected string) {
	t.Helper()
	resolved, err := exec.LookPath(command)
	if err != nil {
		t.Fatalf("resolve %s on PATH: %v", command, err)
	}
	resolvedInfo, err := os.Stat(resolved)
	if err != nil {
		t.Fatalf("inspect resolved %s executable: %v", command, err)
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("inspect expected %s executable: %v", command, err)
	}
	if !os.SameFile(resolvedInfo, expectedInfo) {
		t.Fatalf("%s resolves to %s, want %s", command, resolved, expected)
	}
}
