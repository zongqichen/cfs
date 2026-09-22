package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/install"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/runner"
	"github.com/zongqichen/cfs/internal/store"
	"github.com/zongqichen/cfs/internal/workspace"
)

func runControl(options Options, args []string) int {
	if len(args) == 0 {
		printHelp(options.Stdout)
		return exitOK
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp(options.Stdout)
		return exitOK
	case "setup":
		return commandSetup(options, args[1:])
	case "status":
		return commandStatus(options, args[1:])
	case "doctor":
		return commandDoctor(options, args[1:])
	case "reset":
		return commandReset(options, args[1:])
	case "gc":
		return commandGC(options, args[1:])
	case "uninstall":
		return commandUninstall(options, args[1:])
	case "version", "--version", "-v":
		return commandVersion(options, args[1:])
	default:
		fprintf(options.Stderr, "cfs: unknown command %q\n", args[0])
		fprintf(options.Stderr, "Run 'cfs help' for usage.\n")
		return exitUsage
	}
}

func commandSetup(options Options, args []string) int {
	flags := newFlagSet("setup", options.Stderr)
	realCF := flags.String("real-cf", "", "path to the official CF CLI")
	shimDir := flags.String("shim-dir", "", "directory in which to install the cf shim")
	stateDir := flags.String("state-dir", "", "override the cfs state directory")
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: setup does not accept positional arguments\n")
		return exitUsage
	}

	result, err := install.Setup(install.SetupOptions{
		RealCFPath: *realCF, ShimDir: *shimDir, StateDir: *stateDir,
	})
	if err != nil {
		fprintf(options.Stderr, "cfs: setup failed: %v\n", err)
		return exitError
	}
	if *jsonOutput {
		return writeJSON(options, result)
	}

	fprintf(options.Stdout, "Found official CF CLI: %s\n", result.RealCFPath)
	fprintf(options.Stdout, "Installed shim: %s\n", result.ShimPath)
	fprintf(options.Stdout, "Saved configuration: %s\n", result.ConfigPath)
	if result.PathReady {
		fprintf(options.Stdout, "PATH is ready. Run 'cfs doctor' to verify the installation.\n")
	} else {
		fprintf(options.Stdout, "Add this directory to the beginning of PATH: %s\n", filepath.Dir(result.ShimPath))
	}
	return exitOK
}

type statusOutput struct {
	Mode           string `json:"mode"`
	Workspace      string `json:"workspace,omitempty"`
	Source         string `json:"source"`
	Context        string `json:"context,omitempty"`
	OfficialCF     string `json:"official_cf"`
	CFHome         string `json:"cf_home"`
	TargetOutput   string `json:"target_output"`
	TargetExitCode int    `json:"target_exit_code"`
}

func commandStatus(options Options, args []string) int {
	flags := newFlagSet("status", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: status does not accept positional arguments\n")
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		return reportConfigError(options, err)
	}
	if err := validateRealCF(cfg.RealCFPath); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUnavailable
	}

	status := statusOutput{OfficialCF: cfg.RealCFPath}
	env := os.Environ()
	var workspaceLock *lock.Lock
	if externalHome := os.Getenv("CF_HOME"); externalHome != "" && os.Getenv("CFS_WORKSPACE_ROOT") == "" {
		status.Mode = "external"
		status.Source = "CF_HOME"
		status.CFHome = externalHome
	} else {
		ws, ctx, stateStore, resolveErr := resolveManagedContext(cfg)
		if resolveErr != nil {
			fprintf(options.Stderr, "cfs: %v\n", resolveErr)
			return exitUnavailable
		}
		if err := stateStore.Prepare(ctx); err != nil {
			fprintf(options.Stderr, "cfs: %v\n", err)
			return exitError
		}
		timeout, timeoutErr := lockTimeout()
		if timeoutErr != nil {
			fprintf(options.Stderr, "cfs: %v\n", timeoutErr)
			return exitUsage
		}
		workspaceLock, err = lock.Acquire(ctx.LockPath, timeout)
		if err != nil {
			fprintf(options.Stderr, "cfs: cannot inspect workspace: %v\n", err)
			return exitTemporary
		}
		defer workspaceLock.Release()
		if err := stateStore.Ensure(ctx, ws); err != nil {
			fprintf(options.Stderr, "cfs: %v\n", err)
			return exitError
		}

		status.Mode = "managed"
		status.Workspace = ws.Root
		status.Source = ws.Source
		status.Context = shortID(ctx.ID)
		status.CFHome = ctx.CFHome
		values := map[string]string{"CF_HOME": ctx.CFHome, "CFS_ACTIVE_CONTEXT": ctx.ID}
		if os.Getenv("CF_PLUGIN_HOME") == "" {
			values["CF_PLUGIN_HOME"] = stateStore.SharedPluginHome()
		}
		env = runner.ReplaceEnv(env, values)
	}

	var targetStdout bytes.Buffer
	var targetStderr bytes.Buffer
	result, runErr := runner.Run(cfg.RealCFPath, []string{"target"}, env, runner.IO{
		Stdin: options.Stdin, Stdout: &targetStdout, Stderr: &targetStderr,
	})
	if runErr != nil {
		fprintf(options.Stderr, "cfs: inspect CF target: %v\n", runErr)
		return exitUnavailable
	}
	status.TargetExitCode = result.ExitCode
	targetParts := []string{strings.TrimSpace(targetStdout.String()), strings.TrimSpace(targetStderr.String())}
	status.TargetOutput = strings.TrimSpace(strings.Join(nonEmpty(targetParts), "\n"))

	if *jsonOutput {
		return writeJSON(options, status)
	}
	if status.Workspace != "" {
		fprintf(options.Stdout, "Workspace: %s\n", status.Workspace)
	}
	fprintf(options.Stdout, "Mode: %s\n", status.Mode)
	fprintf(options.Stdout, "Source: %s\n", status.Source)
	if status.Context != "" {
		fprintf(options.Stdout, "Context: %s\n", status.Context)
	}
	fprintf(options.Stdout, "CF CLI: %s\n", status.OfficialCF)
	fprintf(options.Stdout, "CF home: %s\n", status.CFHome)
	fprintf(options.Stdout, "CF target:\n%s\n", status.TargetOutput)
	return result.ExitCode
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorOutput struct {
	Checks []doctorCheck `json:"checks"`
}

func commandDoctor(options Options, args []string) int {
	flags := newFlagSet("doctor", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: doctor does not accept positional arguments\n")
		return exitUsage
	}

	output := doctorOutput{}
	hasFailure := false
	add := func(name, status, message string) {
		output.Checks = append(output.Checks, doctorCheck{Name: name, Status: status, Message: message})
		if status == "fail" {
			hasFailure = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		add("configuration", "fail", err.Error())
	} else {
		configPath, _ := config.FilePath()
		add("configuration", "pass", configPath)
		if err := validateRealCF(cfg.RealCFPath); err != nil {
			add("official-cf", "fail", err.Error())
		} else {
			add("official-cf", "pass", cfg.RealCFPath)
		}

		expectedShim := filepath.Join(cfg.ShimDir, executableName("cf"))
		if info, statErr := os.Lstat(expectedShim); statErr != nil {
			add("shim", "fail", statErr.Error())
		} else if info.Mode()&os.ModeSymlink == 0 {
			add("shim", "fail", "configured shim is not a symbolic link")
		} else {
			target, targetErr := filepath.EvalSymlinks(expectedShim)
			self, selfErr := os.Executable()
			if selfErr == nil {
				self, selfErr = filepath.EvalSymlinks(self)
			}
			if targetErr != nil || selfErr != nil || !pathsEqual(target, self) {
				add("shim", "fail", "configured shim does not point to this cfs executable")
			} else {
				add("shim", "pass", expectedShim)
			}
		}

		pathCF, lookErr := exec.LookPath("cf")
		if lookErr != nil {
			add("path", "fail", "cf is not available on PATH")
		} else if !pathsEqual(pathCF, expectedShim) {
			add("path", "warn", fmt.Sprintf("cf resolves to %s instead of %s", pathCF, expectedShim))
		} else {
			add("path", "pass", pathCF)
		}

		if ws, resolveErr := workspace.Resolve(mustGetwd()); resolveErr != nil {
			add("workspace", "warn", resolveErr.Error())
		} else {
			add("workspace", "pass", ws.Root)
		}
	}

	if *jsonOutput {
		if code := writeJSON(options, output); code != exitOK {
			return code
		}
	} else {
		for _, check := range output.Checks {
			fprintf(options.Stdout, "%-5s %-14s %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
		}
	}
	if hasFailure {
		return exitError
	}
	return exitOK
}

func commandReset(options Options, args []string) int {
	flags := newFlagSet("reset", options.Stderr)
	yes := flags.Bool("yes", false, "confirm reset without prompting")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: reset does not accept positional arguments\n")
		return exitUsage
	}
	if os.Getenv("CF_HOME") != "" {
		fprintf(options.Stderr, "cfs: refusing to reset an externally managed CF_HOME\n")
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		return reportConfigError(options, err)
	}
	ws, ctx, stateStore, err := resolveManagedContext(cfg)
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUnavailable
	}
	if _, err := os.Stat(ctx.Dir); errors.Is(err, os.ErrNotExist) {
		fprintf(options.Stdout, "No managed CF state exists for %s.\n", ws.Root)
		return exitOK
	} else if err != nil {
		fprintf(options.Stderr, "cfs: inspect context: %v\n", err)
		return exitError
	}

	if !*yes {
		if !isTerminal(options.Stdin) {
			fprintf(options.Stderr, "cfs: reset requires --yes when standard input is not a terminal\n")
			return exitUsage
		}
		fprintf(options.Stdout, "Reset CF state for %s? [y/N] ", ws.Root)
		answer, readErr := bufio.NewReader(options.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			fprintf(options.Stderr, "cfs: read confirmation: %v\n", readErr)
			return exitError
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fprintf(options.Stdout, "Reset cancelled.\n")
			return exitOK
		}
	}

	workspaceLock, err := lock.Acquire(ctx.LockPath, 3*time.Second)
	if err != nil {
		fprintf(options.Stderr, "cfs: cannot reset active workspace: %v\n", err)
		return exitTemporary
	}
	destination, moveErr := stateStore.MoveToTrash(ctx)
	releaseErr := workspaceLock.Release()
	if moveErr != nil {
		fprintf(options.Stderr, "cfs: %v\n", moveErr)
		return exitError
	}
	if releaseErr != nil {
		fprintf(options.Stderr, "cfs: %v\n", releaseErr)
		return exitError
	}
	fprintf(options.Stdout, "Moved workspace state to %s\n", destination)
	return exitOK
}

type gcEntry struct {
	Context   string `json:"context"`
	Workspace string `json:"workspace"`
	Action    string `json:"action"`
	Path      string `json:"path,omitempty"`
}

func commandGC(options Options, args []string) int {
	flags := newFlagSet("gc", options.Stderr)
	apply := flags.Bool("apply", false, "move orphaned contexts to trash")
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: gc does not accept positional arguments\n")
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		return reportConfigError(options, err)
	}
	stateRoot, err := config.StateRoot(cfg)
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}
	stateStore := store.New(stateRoot)
	entries, err := stateStore.List()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}

	results := []gcEntry{}
	failed := false
	for _, entry := range entries {
		if !entry.Orphaned {
			continue
		}
		result := gcEntry{Context: shortID(entry.Context.ID), Workspace: entry.Metadata.Workspace, Action: "would-trash"}
		if *apply {
			workspaceLock, lockErr := lock.Acquire(entry.Context.LockPath, 0)
			if lockErr != nil {
				result.Action = "busy"
				failed = true
			} else {
				destination, moveErr := stateStore.MoveToTrash(entry.Context)
				_ = workspaceLock.Release()
				if moveErr != nil {
					result.Action = "error"
					failed = true
				} else {
					result.Action = "trashed"
					result.Path = destination
				}
			}
		}
		results = append(results, result)
	}

	if *jsonOutput {
		if code := writeJSON(options, map[string]any{"contexts": results, "applied": *apply}); code != exitOK {
			return code
		}
	} else if len(results) == 0 {
		fprintf(options.Stdout, "No orphaned workspace contexts found.\n")
	} else {
		for _, result := range results {
			fprintf(options.Stdout, "%-12s %s (%s)\n", result.Action, result.Workspace, result.Context)
		}
		if !*apply {
			fprintf(options.Stdout, "Run 'cfs gc --apply' to move these contexts to trash.\n")
		}
	}
	if failed {
		return exitError
	}
	return exitOK
}

func commandUninstall(options Options, args []string) int {
	flags := newFlagSet("uninstall", options.Stderr)
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: uninstall does not accept positional arguments\n")
		return exitUsage
	}
	path, err := install.Uninstall()
	if err != nil {
		fprintf(options.Stderr, "cfs: uninstall failed: %v\n", err)
		return exitError
	}
	fprintf(options.Stdout, "Removed shim: %s\n", path)
	fprintf(options.Stdout, "Workspace state and the official CF CLI were preserved.\n")
	return exitOK
}

func commandVersion(options Options, args []string) int {
	flags := newFlagSet("version", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: version does not accept positional arguments\n")
		return exitUsage
	}
	value := map[string]string{"version": options.Version, "commit": options.Commit, "build_date": options.BuildDate}
	if *jsonOutput {
		return writeJSON(options, value)
	}
	fprintf(options.Stdout, "cfs %s (commit %s, built %s)\n", options.Version, options.Commit, options.BuildDate)
	return exitOK
}

func resolveManagedContext(cfg config.Config) (workspace.Workspace, store.Context, store.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return workspace.Workspace{}, store.Context{}, store.Store{}, fmt.Errorf("resolve current directory: %w", err)
	}
	ws, err := workspace.Resolve(cwd)
	if err != nil {
		return workspace.Workspace{}, store.Context{}, store.Store{}, err
	}
	stateRoot, err := config.StateRoot(cfg)
	if err != nil {
		return workspace.Workspace{}, store.Context{}, store.Store{}, err
	}
	stateStore := store.New(stateRoot)
	ctx, err := stateStore.ContextFor(ws)
	if err != nil {
		return workspace.Workspace{}, store.Context{}, store.Store{}, err
	}
	return ws, ctx, stateStore, nil
}

func reportConfigError(options Options, err error) int {
	if errors.Is(err, config.ErrNotConfigured) {
		fprintf(options.Stderr, "cfs: not configured; run 'cfs setup' first\n")
	} else {
		fprintf(options.Stderr, "cfs: %v\n", err)
	}
	return exitUnavailable
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet("cfs "+name, flag.ContinueOnError)
	flags.SetOutput(output)
	return flags
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
	fprintf(output, `cfs keeps Cloud Foundry CLI state isolated per workspace.

Usage:
  cfs <command> [options]

Commands:
  setup       Install and configure the transparent cf shim
  status      Show workspace resolution and the current CF target
  doctor      Diagnose configuration and installation problems
  reset       Move the current workspace state to recoverable trash
  gc          Find or trash state for workspaces that no longer exist
  uninstall   Remove the transparent shim without deleting state
  version     Print version information

Environment:
  CFS_WORKSPACE_ROOT  Override automatic workspace discovery
  CFS_STATE_HOME      Override the state directory
  CFS_LOCK_TIMEOUT    Maximum wait for a workspace lock (default: 3s)
  CFS_DISABLE=1       Bypass workspace isolation for one invocation

Normal Cloud Foundry commands remain unchanged:
  cf login --sso
  cf target -o my-org -s my-space
  cf apps
`)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func executableName(base string) string {
	if strings.EqualFold(filepath.Ext(os.Args[0]), ".exe") {
		return base + ".exe"
	}
	return base
}

func pathsEqual(first, second string) bool {
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	if firstErr != nil || secondErr != nil {
		return false
	}
	return filepath.Clean(firstAbs) == filepath.Clean(secondAbs)
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func isTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func nonEmpty(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
