package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestGlobalHelpAliases(t *testing.T) {
	baseline := runControlCommand(t)
	if baseline.code != exitOK || baseline.stdout == "" || baseline.stderr != "" {
		t.Fatalf("cfs help result = %#v", baseline)
	}

	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		result := runControlCommand(t, args...)
		if result != baseline {
			t.Errorf("cfs %s result differs from cfs: %#v", strings.Join(args, " "), result)
		}
	}

	for _, command := range allCommands() {
		if !strings.Contains(baseline.stdout, command.name) {
			t.Errorf("global help does not list %q", command.name)
		}
	}
}

func TestEveryCommandHasConsistentHelp(t *testing.T) {
	for _, command := range allCommands() {
		t.Run(command.name, func(t *testing.T) {
			fromHelp := runControlCommand(t, "help", command.name)
			fromLongFlag := runControlCommand(t, command.name, "--help")
			fromShortFlag := runControlCommand(t, command.name, "-h")

			for invocation, result := range map[string]commandResult{
				"help command": fromHelp,
				"long flag":    fromLongFlag,
				"short flag":   fromShortFlag,
			} {
				if result.code != exitOK || result.stdout == "" || result.stderr != "" {
					t.Errorf("%s result = %#v", invocation, result)
				}
			}
			if fromHelp.stdout != fromLongFlag.stdout || fromHelp.stdout != fromShortFlag.stdout {
				t.Errorf("help output differs:\nhelp command:\n%s\nlong flag:\n%s\nshort flag:\n%s", fromHelp.stdout, fromLongFlag.stdout, fromShortFlag.stdout)
			}
			if !strings.Contains(fromHelp.stdout, command.summary) {
				t.Errorf("help for %q does not contain its summary: %q", command.name, fromHelp.stdout)
			}
		})
	}
}

func TestHelpRejectsUnknownTopic(t *testing.T) {
	result := runControlCommand(t, "help", "missing")
	if result.code != exitUsage {
		t.Fatalf("exit code = %d, want %d", result.code, exitUsage)
	}
	if result.stdout != "" || !strings.Contains(result.stderr, `unknown help topic "missing"`) {
		t.Fatalf("result = %#v", result)
	}
}

func TestHelpRejectsExtraArguments(t *testing.T) {
	result := runControlCommand(t, "help", "setup", "status")
	if result.code != exitUsage {
		t.Fatalf("exit code = %d, want %d", result.code, exitUsage)
	}
	if result.stdout != "" || !strings.Contains(result.stderr, "at most one command name") {
		t.Fatalf("result = %#v", result)
	}
}

func TestVersionAliases(t *testing.T) {
	want := runControlCommand(t, "version")
	for _, alias := range []string{"-v", "--version"} {
		if got := runControlCommand(t, alias); got != want {
			t.Errorf("cfs %s result = %#v, want %#v", alias, got, want)
		}
	}
}

func runControlCommand(t *testing.T, args ...string) commandResult {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(Options{
		Args:      append([]string{"cfs"}, args...),
		Stdin:     strings.NewReader(""),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Version:   "test-version",
		Commit:    "test-commit",
		BuildDate: "test-date",
	})
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}
