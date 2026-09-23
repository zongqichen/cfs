//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/lock"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/store"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/workspace"
)

func TestNamedContextsUseIndependentHomesAndKeepDefaultCompatible(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	root := markerWorkspace(t)

	defaultShim := runFromDirectory(t, root, []string{"cf", "apps"})
	requireAppSuccess(t, "default shim", defaultShim)
	createNamedContext(t, root, "prod")
	createNamedContext(t, root, "poc")

	defaultNamed := runFromDirectory(t, root, []string{"cfs", "-c", "default", "apps"})
	prod := runFromDirectory(t, root, []string{"cfs", "-c", "prod", "apps"})
	poc := runFromDirectory(t, root, []string{"cfs", "--context=poc", "apps"})
	for name, result := range map[string]commandResult{"explicit default": defaultNamed, "prod": prod, "poc": poc} {
		requireAppSuccess(t, name, result)
	}

	defaultHome := outputValue(defaultShim.stdout, "CF_HOME")
	if got := outputValue(defaultNamed.stdout, "CF_HOME"); got != defaultHome {
		t.Fatalf("explicit default CF_HOME = %q, want %q", got, defaultHome)
	}
	prodHome := outputValue(prod.stdout, "CF_HOME")
	pocHome := outputValue(poc.stdout, "CF_HOME")
	if prodHome == defaultHome || pocHome == defaultHome || prodHome == pocHome {
		t.Fatalf("contexts shared CF_HOME: default=%q prod=%q poc=%q", defaultHome, prodHome, pocHome)
	}
}

func TestNamedContextLifecycle(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, canonicalTestPath(t, t.TempDir()))
	root := markerWorkspace(t)

	before := runFromDirectory(t, root, []string{"cfs", "context", "list", "--json"})
	requireAppSuccess(t, "initial context list", before)
	assertContextList(t, before.stdout, map[string]bool{"default": false})

	createNamedContext(t, root, "prod")
	duplicate := runFromDirectory(t, root, []string{"cfs", "context", "create", "prod"})
	if duplicate.code != exitUsage || !strings.Contains(duplicate.stderr, "already exists") {
		t.Fatalf("duplicate create = %#v", duplicate)
	}

	listed := runFromDirectory(t, root, []string{"cfs", "context", "list", "--json"})
	requireAppSuccess(t, "context list", listed)
	assertContextList(t, listed.stdout, map[string]bool{"default": false, "prod": true})

	status := runFromDirectory(t, root, []string{"cfs", "context", "status", "prod", "--json", "--redact"})
	requireAppSuccess(t, "context status", status)
	var statusOutput statusOutput
	if err := json.Unmarshal([]byte(status.stdout), &statusOutput); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if statusOutput.ContextName != "prod" || statusOutput.Context == "" || !statusOutput.Redacted {
		t.Fatalf("context status = %#v", statusOutput)
	}

	removed := runFromDirectory(t, root, []string{"cfs", "context", "remove", "prod", "--yes"})
	requireAppSuccess(t, "context remove", removed)
	missing := runFromDirectory(t, root, []string{"cfs", "-c", "prod", "apps"})
	if missing.code != exitUnavailable || !strings.Contains(missing.stderr, `context "prod" does not exist`) {
		t.Fatalf("removed context invocation = %#v", missing)
	}
}

func TestNamedInvocationFailsClosedWithoutCreatingState(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	configureTestEnvironment(t, fakeCF, stateRoot)
	root := markerWorkspace(t)

	unknown := runFromDirectory(t, root, []string{"cfs", "-c", "missing", "apps"})
	if unknown.code != exitUnavailable || !strings.Contains(unknown.stderr, `context "missing" does not exist`) {
		t.Fatalf("unknown context invocation = %#v", unknown)
	}
	invalid := runFromDirectory(t, root, []string{"cfs", "-c", "../prod", "apps"})
	if invalid.code != exitUsage || !strings.Contains(invalid.stderr, "invalid context name") {
		t.Fatalf("invalid context invocation = %#v", invalid)
	}
	entries, err := store.New(stateRoot).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed invocations created contexts: %#v", entries)
	}
}

func TestNamedContextLocksAreIndependent(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	root := markerWorkspace(t)
	createNamedContext(t, root, "prod")
	createNamedContext(t, root, "poc")

	ws, err := workspace.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	prod, err := store.New(stateRoot).ContextForName(ws, "prod")
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(prod.LockPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	busy := runFromDirectory(t, root, []string{"cfs", "-c", "prod", "apps"})
	if busy.code != exitTemporary || !strings.Contains(busy.stderr, "active CF command") {
		t.Fatalf("same-context command = %#v", busy)
	}
	independent := runFromDirectory(t, root, []string{"cfs", "-c", "poc", "apps"})
	requireAppSuccess(t, "different-context command", independent)
}

func TestBusyContextCreateDoesNotLeaveIncompleteState(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	root := markerWorkspace(t)
	ws, err := workspace.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	stateStore := store.New(stateRoot)
	ctx, err := stateStore.ContextForName(ws, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.PrepareRoot(); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(ctx.LockPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	created := runFromDirectory(t, root, []string{"cfs", "context", "create", "prod"})
	if created.code != exitTemporary {
		t.Fatalf("busy context create = %#v", created)
	}
	if _, err := os.Stat(ctx.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("busy create left context state: %v", err)
	}
	listed := runFromDirectory(t, root, []string{"cfs", "context", "list", "--json"})
	requireAppSuccess(t, "context list after busy create", listed)
	assertContextList(t, listed.stdout, map[string]bool{"default": false})
}

func TestNamedInvocationRejectsExternalCFHome(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, canonicalTestPath(t, t.TempDir()))
	root := markerWorkspace(t)
	createNamedContext(t, root, "prod")
	t.Setenv("CF_HOME", t.TempDir())

	result := runFromDirectory(t, root, []string{"cfs", "-c", "prod", "apps"})
	if result.code != exitUsage || !strings.Contains(result.stderr, "unset CF_HOME") || result.stdout != "" {
		t.Fatalf("named invocation with external CF_HOME = %#v", result)
	}
}

func TestShimAcceptsValidatedNestedNamedContext(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, canonicalTestPath(t, t.TempDir()))
	root := markerWorkspace(t)
	createNamedContext(t, root, "prod")

	named := runFromDirectory(t, root, []string{"cfs", "-c", "prod", "apps"})
	requireAppSuccess(t, "named invocation", named)
	home := outputValue(named.stdout, "CF_HOME")
	t.Setenv("CFS_ACTIVE_CONTEXT", filepath.Base(filepath.Dir(home)))
	t.Setenv("CF_HOME", home)
	t.Setenv("CFS_WORKSPACE_ROOT", root)

	nested := runFromDirectory(t, t.TempDir(), []string{"cf", "apps"})
	requireAppSuccess(t, "nested named invocation", nested)
	if got := outputValue(nested.stdout, "CF_HOME"); got != home {
		t.Fatalf("nested CF_HOME = %q, want %q", got, home)
	}
}

func TestActivateExistingReportsContextRemovedWhileWaitingForLock(t *testing.T) {
	stateRoot := canonicalTestPath(t, t.TempDir())
	stateStore := store.New(stateRoot)
	ws := workspace.Workspace{
		Root: markerWorkspace(t), Source: "test",
		ID: strings.Repeat("4", 64), Fingerprint: "fingerprint",
	}
	ctx, err := stateStore.ContextForName(ws, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Ensure(ctx, ws); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(ctx.LockPath, 0)
	if err != nil {
		t.Fatal(err)
	}

	type activationResult struct {
		contextLock *lock.Lock
		err         error
	}
	result := make(chan activationResult, 1)
	go func() {
		contextLock, activateErr := (managedContext{Workspace: ws, Context: ctx, Store: stateStore}).activateExisting(time.Second)
		result <- activationResult{contextLock: contextLock, err: activateErr}
	}()
	time.Sleep(2 * lockRetryIntervalForTest)
	if _, err := stateStore.MoveToTrash(ctx); err != nil {
		t.Fatal(err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}

	activation := <-result
	if activation.contextLock != nil {
		_ = activation.contextLock.Release()
		t.Fatal("activateExisting() returned a lock for a removed context")
	}
	if !errors.Is(activation.err, errContextNotFound) {
		t.Fatalf("activateExisting() error = %v, want errContextNotFound", activation.err)
	}
}

const lockRetryIntervalForTest = 50 * time.Millisecond

func createNamedContext(t *testing.T, root, name string) {
	t.Helper()
	result := runFromDirectory(t, root, []string{"cfs", "context", "create", name})
	requireAppSuccess(t, "create context "+name, result)
}

func requireAppSuccess(t *testing.T, operation string, result commandResult) {
	t.Helper()
	if result.code != exitOK {
		t.Fatalf("%s failed: code=%d stdout=%q stderr=%q", operation, result.code, result.stdout, result.stderr)
	}
}

func assertContextList(t *testing.T, raw string, expected map[string]bool) {
	t.Helper()
	var output struct {
		Contexts []contextListItem `json:"contexts"`
	}
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		t.Fatalf("decode context list: %v", err)
	}
	if len(output.Contexts) != len(expected) {
		t.Fatalf("contexts = %#v, want %d entries", output.Contexts, len(expected))
	}
	for _, item := range output.Contexts {
		wantCreated, found := expected[item.Name]
		if !found || item.Created != wantCreated {
			t.Fatalf("unexpected context item %#v", item)
		}
	}
}
