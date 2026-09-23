package app

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"slices"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/envvar"
)

const (
	shortContextIDLength   = 8
	trashCredentialWarning = "Warning: trashed CF state may contain active credentials and is not purged automatically."
)

type commandHandler func(Options, []string) int

type commandSpec struct {
	name     string
	aliases  []string
	summary  string
	usage    string
	examples string
	run      commandHandler
}

func allCommands() []commandSpec {
	return []commandSpec{
		{
			name:     "setup",
			summary:  "Install and configure the transparent cf shim",
			usage:    "cfs setup [options]",
			examples: "  cfs setup\n  cfs setup --real-cf /absolute/path/to/cf",
			run:      commandSetup,
		},
		{
			name:     "status",
			summary:  "Show workspace resolution and the current CF target",
			usage:    "cfs status [--json] [--redact]",
			examples: "  cfs status\n  cfs status --json --redact",
			run:      commandStatus,
		},
		{
			name:     "context",
			summary:  "Manage named Cloud Foundry contexts in the current workspace",
			usage:    "cfs context <create|list|status|remove> [options]",
			examples: "  cfs context create prod\n  cfs context list\n  cfs context status prod --json --redact",
			run:      commandContext,
		},
		{
			name:     "import",
			summary:  "Import global CF state into a workspace context",
			usage:    "cfs import [--context <name>] [--yes] [--force]",
			examples: "  cfs import\n  cfs import --yes\n  cfs import --context prod --yes",
			run:      commandImport,
		},
		{
			name:     "doctor",
			summary:  "Diagnose configuration and installation problems",
			usage:    "cfs doctor [--json]",
			examples: "  cfs doctor",
			run:      commandDoctor,
		},
		{
			name:     "reset",
			summary:  "Move the current workspace state to recoverable trash",
			usage:    "cfs reset [--yes]",
			examples: "  cfs reset\n  cfs reset --yes",
			run:      commandReset,
		},
		{
			name:     "gc",
			summary:  "Find or trash state for workspaces that no longer exist",
			usage:    "cfs gc [--apply] [--json]",
			examples: "  cfs gc\n  cfs gc --apply",
			run:      commandGC,
		},
		{
			name:    "uninstall",
			summary: "Remove the transparent shim without deleting state",
			usage:   "cfs uninstall",
			run:     commandUninstall,
		},
		{
			name:     "version",
			aliases:  []string{"-v", "--version"},
			summary:  "Print version information",
			usage:    "cfs version [--json]",
			examples: "  cfs version",
			run:      commandVersion,
		},
		{
			name:     "update",
			summary:  "Check for a newer cfs release",
			usage:    "cfs update [--json]",
			examples: "  cfs update\n  cfs update --json",
			run:      commandUpdate,
		},
		{
			name:     "help",
			summary:  "Show help for cfs or a command",
			usage:    "cfs help [command]",
			examples: "  cfs help\n  cfs help setup",
			run:      commandHelp,
		},
	}
}

func runControl(options Options, args []string) int {
	if len(args) == 0 {
		printHelp(options.Stdout)
		return exitOK
	}
	if args[0] == "-h" || args[0] == "--help" {
		printHelp(options.Stdout)
		return exitOK
	}

	command, ok := findCommand(args[0])
	if !ok {
		fprintf(options.Stderr, "cfs: unknown command %q\n", args[0])
		fprintf(options.Stderr, "Run 'cfs help' for usage.\n")
		return exitUsage
	}
	commandArgs := args[1:]
	if isHelpRequest(commandArgs) {
		options.Stderr = options.Stdout
	}
	exitCode := command.run(options, commandArgs)
	if exitCode == exitOK {
		maybeNotifyUpdate(options, command, commandArgs)
	}
	return exitCode
}

func commandHelp(options Options, args []string) int {
	if len(args) == 0 {
		printHelp(options.Stdout)
		return exitOK
	}
	if len(args) != 1 {
		fprintf(options.Stderr, "cfs: help accepts at most one command name\n")
		return exitUsage
	}

	topic := args[0]
	if isHelpRequest(args) {
		topic = "help"
	}
	command, ok := findCommand(topic)
	if !ok {
		fprintf(options.Stderr, "cfs: unknown help topic %q\n", topic)
		fprintf(options.Stderr, "Run 'cfs help' to list commands.\n")
		return exitUsage
	}
	if command.name == "help" {
		flags := newFlagSet(command.name, options.Stdout)
		printCommandHelp(options.Stdout, command, flags)
		return exitOK
	}
	options.Stderr = options.Stdout
	return command.run(options, []string{"--help"})
}

func findCommand(name string) (commandSpec, bool) {
	for _, command := range allCommands() {
		if command.name == name || slices.Contains(command.aliases, name) {
			return command, true
		}
	}
	return commandSpec{}, false
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "-h" || args[0] == "--help")
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet("cfs "+name, flag.ContinueOnError)
	flags.SetOutput(output)
	if command, ok := findCommand(name); ok {
		flags.Usage = func() { printCommandHelp(output, command, flags) }
	}
	return flags
}

func printCommandHelp(output io.Writer, command commandSpec, flags *flag.FlagSet) {
	fprintf(output, "%s.\n\nUsage:\n  %s\n", command.summary, command.usage)
	hasOptions := false
	flags.VisitAll(func(*flag.Flag) { hasOptions = true })
	if hasOptions {
		fprintf(output, "\nOptions:\n")
		flags.PrintDefaults()
	}
	if command.examples != "" {
		fprintf(output, "\nExamples:\n%s\n", command.examples)
	}
}

func parseFlagSet(flags *flag.FlagSet, args []string) (int, bool) {
	err := flags.Parse(args)
	if err == nil {
		return exitOK, true
	}
	if errors.Is(err, flag.ErrHelp) {
		return exitOK, false
	}
	return exitUsage, false
}

func reportConfigError(options Options, err error) int {
	if errors.Is(err, config.ErrNotConfigured) {
		fprintf(options.Stderr, "cfs: not configured; run 'cfs setup' first\n")
	} else {
		fprintf(options.Stderr, "cfs: %v\n", err)
	}
	return exitUnavailable
}

func writeJSON(options Options, value any) int {
	encoder := json.NewEncoder(options.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fprintf(options.Stderr, "cfs: encode JSON output: %v\n", err)
		return exitError
	}
	return exitOK
}

func printHelp(output io.Writer) {
	fprintf(output, "cfs keeps Cloud Foundry CLI state isolated per workspace.\n\n")
	fprintf(output, "Usage:\n  cfs <command> [options]\n  cfs -c <name> <cf arguments...>\n  cfs help <command>\n\nCommands:\n")
	for _, command := range allCommands() {
		fprintf(output, "  %-11s %s\n", command.name, command.summary)
	}
	fprintf(output, "\nEnvironment:\n  %s  Override automatic workspace discovery\n  %s      Override the state directory\n  %s    Maximum wait for a context lock (default: %s)\n  %s=1       Bypass workspace isolation for one invocation\n  %s=1 Disable interactive update notices\n\nNormal Cloud Foundry commands remain unchanged:\n  cf login --sso\n  cf target -o my-org -s my-space\n  cf apps\n", envvar.WorkspaceRoot, envvar.StateHome, envvar.LockTimeout, defaultLockTimeout, envvar.Disable, envvar.NoUpdateCheck)
}

func shortID(id string) string {
	if len(id) <= shortContextIDLength {
		return id
	}
	return id[:shortContextIDLength]
}
