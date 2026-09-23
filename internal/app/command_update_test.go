package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/envvar"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/updatecheck"
)

func TestUpdateReportsAvailableRelease(t *testing.T) {
	updates := &fakeUpdateService{result: availableUpdate()}
	result := runUpdateCommand(t, updates)

	if result.code != exitOK || result.stderr != "" {
		t.Fatalf("cfs update result = %#v", result)
	}
	for _, expected := range []string{
		"Update available: 0.2.0 -> 0.3.0",
		"https://github.com/zongqichen/cloud-foundry-cli-contexts/releases/tag/v0.3.0",
		"go install github.com/zongqichen/cloud-foundry-cli-contexts/cmd/cfs@v0.3.0",
		"cfs setup",
		"cfs doctor",
	} {
		if !strings.Contains(result.stdout, expected) {
			t.Errorf("stdout = %q, want %q", result.stdout, expected)
		}
	}
	if updates.checks != 1 || updates.notifications != 0 {
		t.Fatalf("service calls = checks %d, notifications %d", updates.checks, updates.notifications)
	}
}

func TestUpdateJSONIsStableAndMachineReadable(t *testing.T) {
	updates := &fakeUpdateService{result: availableUpdate()}
	result := runUpdateCommand(t, updates, "--json")

	if result.code != exitOK || result.stderr != "" {
		t.Fatalf("cfs update --json result = %#v", result)
	}
	var decoded updatecheck.Result
	if err := json.Unmarshal([]byte(result.stdout), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != updatecheck.StatusUpdateAvailable || !decoded.UpdateAvailable || len(decoded.Commands) != 3 {
		t.Fatalf("JSON result = %#v", decoded)
	}
}

func TestUpdateReportsOtherVersionStates(t *testing.T) {
	tests := []struct {
		name   string
		result updatecheck.Result
		want   string
	}{
		{
			name:   "up to date",
			result: updatecheck.Result{CurrentVersion: "0.2.0", LatestVersion: "0.2.0", Status: updatecheck.StatusUpToDate},
			want:   "cfs 0.2.0 is up to date.",
		},
		{
			name:   "ahead",
			result: updatecheck.Result{CurrentVersion: "0.3.0", LatestVersion: "0.2.0", Status: updatecheck.StatusAhead},
			want:   "newer than the latest published release",
		},
		{
			name:   "development",
			result: updatecheck.Result{CurrentVersion: "dev", LatestVersion: "0.2.0", Status: updatecheck.StatusDevelopment},
			want:   "cannot be compared with published releases",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runUpdateCommand(t, &fakeUpdateService{result: test.result})
			if result.code != exitOK || !strings.Contains(result.stdout, test.want) {
				t.Fatalf("result = %#v, want %q", result, test.want)
			}
		})
	}
}

func TestUpdateFailsClearlyWhenReleaseCheckFails(t *testing.T) {
	result := runUpdateCommand(t, &fakeUpdateService{err: errors.New("offline")})
	if result.code != exitUnavailable || result.stdout != "" || !strings.Contains(result.stderr, "check for updates: offline") {
		t.Fatalf("result = %#v", result)
	}
}

func TestUpdateRejectsArguments(t *testing.T) {
	updates := &fakeUpdateService{}
	result := runUpdateCommand(t, updates, "unexpected")
	if result.code != exitUsage || updates.checks != 0 {
		t.Fatalf("result = %#v, checks = %d", result, updates.checks)
	}
}

func TestInteractiveControlCommandShowsOneUpdateNotice(t *testing.T) {
	t.Setenv(envvar.CI, "")
	t.Setenv(envvar.NoUpdateCheck, "")
	updates := &fakeUpdateService{result: availableUpdate(), notify: true}
	result := runCommandWithUpdates(t, updates, true, "version")
	if result.code != exitOK || !strings.Contains(result.stderr, "run 'cfs update'") {
		t.Fatalf("result = %#v", result)
	}
	if updates.checks != 0 || updates.notifications != 1 {
		t.Fatalf("service calls = checks %d, notifications %d", updates.checks, updates.notifications)
	}
}

func TestPassiveUpdateNoticeNeverChangesCommandFailure(t *testing.T) {
	t.Setenv(envvar.CI, "")
	t.Setenv(envvar.NoUpdateCheck, "")
	updates := &fakeUpdateService{err: errors.New("offline"), notify: true}
	result := runCommandWithUpdates(t, updates, true, "version")
	if result.code != exitOK || result.stderr != "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestPassiveUpdateNoticeIsSuppressed(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		args        []string
		environment map[string]string
	}{
		{name: "non-interactive", args: []string{"version"}},
		{name: "JSON", interactive: true, args: []string{"version", "--json"}},
		{name: "help", interactive: true, args: []string{"help"}},
		{name: "command help", interactive: true, args: []string{"version", "--help"}},
		{name: "opt out", interactive: true, args: []string{"version"}, environment: map[string]string{envvar.NoUpdateCheck: "1"}},
		{name: "CI", interactive: true, args: []string{"version"}, environment: map[string]string{envvar.CI: "true"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(envvar.CI, "")
			t.Setenv(envvar.NoUpdateCheck, "")
			for name, value := range test.environment {
				t.Setenv(name, value)
			}
			updates := &fakeUpdateService{result: availableUpdate(), notify: true}
			result := runCommandWithUpdates(t, updates, test.interactive, test.args...)
			if result.code != exitOK || updates.notifications != 0 {
				t.Fatalf("result = %#v, notifications = %d", result, updates.notifications)
			}
		})
	}
}

func TestOutputTerminalRejectsNonTTYCharacterDevice(t *testing.T) {
	device, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()

	if isOutputTerminal(device) {
		t.Fatalf("isOutputTerminal(%s) = true, want false", os.DevNull)
	}
}

func TestCFShimNeverChecksForUpdates(t *testing.T) {
	t.Setenv(envvar.ConfigFile, filepath.Join(t.TempDir(), "missing.json"))
	updates := &fakeUpdateService{result: availableUpdate(), notify: true}
	var stderr bytes.Buffer
	code := Run(Options{
		Args:          []string{"cf", "apps"},
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
		Stderr:        &stderr,
		Version:       "0.2.0",
		Updates:       updates,
		IsInteractive: func(io.Writer) bool { return true },
	})
	if code != exitUnavailable || updates.checks != 0 || updates.notifications != 0 {
		t.Fatalf("code = %d, service calls = checks %d, notifications %d", code, updates.checks, updates.notifications)
	}
}

func TestNamedCFInvocationNeverChecksForUpdates(t *testing.T) {
	t.Setenv(envvar.ConfigFile, filepath.Join(t.TempDir(), "missing.json"))
	updates := &fakeUpdateService{result: availableUpdate(), notify: true}
	var stderr bytes.Buffer
	code := Run(Options{
		Args:          []string{"cfs", "-c", "qa", "apps"},
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
		Stderr:        &stderr,
		Version:       "0.2.0",
		Updates:       updates,
		IsInteractive: func(io.Writer) bool { return true },
	})
	if code != exitUnavailable || updates.checks != 0 || updates.notifications != 0 {
		t.Fatalf("code = %d, service calls = checks %d, notifications %d", code, updates.checks, updates.notifications)
	}
}

type fakeUpdateService struct {
	result        updatecheck.Result
	err           error
	notify        bool
	checks        int
	notifications int
}

func (service *fakeUpdateService) Check(context.Context, string) (updatecheck.Result, error) {
	service.checks++
	return service.result, service.err
}

func (service *fakeUpdateService) Notification(context.Context, string) (updatecheck.Result, bool, error) {
	service.notifications++
	return service.result, service.notify, service.err
}

func availableUpdate() updatecheck.Result {
	return updatecheck.Result{
		CurrentVersion:  "0.2.0",
		LatestVersion:   "0.3.0",
		UpdateAvailable: true,
		Status:          updatecheck.StatusUpdateAvailable,
		ReleaseURL:      "https://github.com/zongqichen/cloud-foundry-cli-contexts/releases/tag/v0.3.0",
		Commands: []string{
			"go install github.com/zongqichen/cloud-foundry-cli-contexts/cmd/cfs@v0.3.0",
			"cfs setup",
			"cfs doctor",
		},
	}
}

func runUpdateCommand(t *testing.T, updates UpdateService, args ...string) commandResult {
	t.Helper()
	return runCommandWithUpdates(t, updates, false, append([]string{"update"}, args...)...)
}

func runCommandWithUpdates(t *testing.T, updates UpdateService, interactive bool, args ...string) commandResult {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(Options{
		Args:          append([]string{"cfs"}, args...),
		Stdin:         strings.NewReader(""),
		Stdout:        &stdout,
		Stderr:        &stderr,
		Version:       "0.2.0",
		Commit:        "test-commit",
		BuildDate:     "test-date",
		Updates:       updates,
		IsInteractive: func(io.Writer) bool { return interactive },
	})
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}
