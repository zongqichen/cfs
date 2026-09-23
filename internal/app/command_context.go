package app

import (
	"errors"
	"flag"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/config"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/contextname"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/lock"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/store"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/workspace"
)

type contextListItem struct {
	Name    string `json:"name"`
	Context string `json:"context"`
	Default bool   `json:"default"`
	Created bool   `json:"created"`
}

func commandContext(options Options, args []string) int {
	if len(args) == 0 || isHelpRequest(args) {
		printContextHelp(options.Stdout)
		return exitOK
	}
	if args[0] == "help" {
		if len(args) == 1 {
			printContextHelp(options.Stdout)
			return exitOK
		}
		args = append([]string{args[1], "--help"}, args[2:]...)
	}

	switch args[0] {
	case "create":
		return commandContextCreate(options, args[1:])
	case "list", "ls":
		return commandContextList(options, args[1:])
	case "status":
		return commandContextStatus(options, args[1:])
	case "remove", "rm":
		return commandContextRemove(options, args[1:])
	default:
		fprintf(options.Stderr, "cfs: unknown context command %q\n", args[0])
		fprintf(options.Stderr, "Run 'cfs help context' for usage.\n")
		return exitUsage
	}
}

func commandContextCreate(options Options, args []string) (exitCode int) {
	if isHelpRequest(args) {
		printContextSubcommandHelp(options.Stdout, "Create an empty named context.", "cfs context create <name>")
		return exitOK
	}
	if len(args) != 1 {
		fprintf(options.Stderr, "cfs: context create requires exactly one name\n")
		return exitUsage
	}
	name := args[0]
	if err := contextname.Validate(name); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	managed, code, ok := managedContextForCommand(options, name)
	if !ok {
		return code
	}
	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	if err := managed.Store.PrepareRoot(); err != nil {
		fprintf(options.Stderr, "cfs: prepare context store: %v\n", err)
		return exitError
	}
	contextLock, err := lock.Acquire(managed.Context.LockPath, timeout)
	if errors.Is(err, lock.ErrBusy) {
		fprintf(options.Stderr, "cfs: context %q already has an active operation\n", name)
		return exitTemporary
	}
	if err != nil {
		fprintf(options.Stderr, "cfs: create context %q: %v\n", name, err)
		return exitError
	}
	defer func() {
		if releaseErr := contextLock.Release(); releaseErr != nil {
			fprintf(options.Stderr, "cfs: %v\n", releaseErr)
			if exitCode == exitOK {
				exitCode = exitError
			}
		}
	}()
	if _, err := managed.Store.ValidateForWorkspace(managed.Context, managed.Workspace); err == nil {
		fprintf(options.Stderr, "cfs: context %q already exists\n", name)
		return exitUsage
	} else if !errors.Is(err, os.ErrNotExist) {
		fprintf(options.Stderr, "cfs: inspect context %q: %v\n", name, err)
		return exitError
	}
	if err := managed.Store.Ensure(managed.Context, managed.Workspace); err != nil {
		fprintf(options.Stderr, "cfs: create context %q: %v\n", name, err)
		return exitError
	}
	fprintf(options.Stdout, "Created context %q.\n", name)
	return exitOK
}

func commandContextList(options Options, args []string) int {
	flags := newContextFlagSet("list", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: context list does not accept positional arguments\n")
		return exitUsage
	}
	cfg, err := config.Load()
	if err != nil {
		return reportConfigError(options, err)
	}
	ws, err := workspace.Resolve(currentDirectory())
	if err != nil {
		reportWorkspaceError(options, err)
		return exitUnavailable
	}
	stateRoot, err := config.StateRoot(cfg)
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}
	stateStore := store.New(stateRoot)
	entries, err := stateStore.ListForWorkspace(ws)
	if err != nil {
		fprintf(options.Stderr, "cfs: list contexts: %v\n", err)
		return exitError
	}
	items, err := contextItems(stateStore, ws, entries)
	if err != nil {
		fprintf(options.Stderr, "cfs: list contexts: %v\n", err)
		return exitError
	}
	if *jsonOutput {
		return writeJSON(options, map[string]any{"contexts": items})
	}
	printContextList(options.Stdout, items)
	return exitOK
}

func commandContextStatus(options Options, args []string) int {
	if len(args) == 0 {
		fprintf(options.Stderr, "cfs: context status requires a name\n")
		return exitUsage
	}
	if isHelpRequest(args) {
		printContextSubcommandHelp(options.Stdout, "Show a named context and its CF target.", "cfs context status <name> [--json] [--redact]")
		return exitOK
	}
	name := args[0]
	if err := contextname.Validate(name); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	flags := newContextFlagSet("status", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	redact := flags.Bool("redact", false, "omit paths and CF target details")
	if code, ok := parseFlagSet(flags, args[1:]); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: context status does not accept extra arguments\n")
		return exitUsage
	}
	return reportStatus(options, name, true, *jsonOutput, *redact)
}

func commandContextRemove(options Options, args []string) int {
	if len(args) == 0 {
		fprintf(options.Stderr, "cfs: context remove requires a name\n")
		return exitUsage
	}
	if isHelpRequest(args) {
		printContextSubcommandHelp(options.Stdout, "Move a named context to recoverable trash.", "cfs context remove <name> [--yes]")
		return exitOK
	}
	name := args[0]
	if err := contextname.Validate(name); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	flags := newContextFlagSet("remove", options.Stderr)
	yes := flags.Bool("yes", false, "confirm removal without prompting")
	if code, ok := parseFlagSet(flags, args[1:]); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: context remove does not accept extra arguments\n")
		return exitUsage
	}
	managed, code, ok := managedContextForCommand(options, name)
	if !ok {
		return code
	}
	if _, err := managed.Store.ValidateForWorkspace(managed.Context, managed.Workspace); errors.Is(err, os.ErrNotExist) {
		fprintf(options.Stderr, "cfs: context %q does not exist\n", name)
		return exitUnavailable
	} else if err != nil {
		fprintf(options.Stderr, "cfs: inspect context %q: %v\n", name, err)
		return exitError
	}
	if !*yes {
		confirmed, code := confirm(options, "context remove", "Remove context "+name+" from "+managed.Workspace.Root+"?")
		if !confirmed {
			return code
		}
	}
	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	contextLock, err := lock.Acquire(managed.Context.LockPath, timeout)
	if errors.Is(err, lock.ErrBusy) {
		fprintf(options.Stderr, "cfs: context %q already has an active CF command\n", name)
		return exitTemporary
	}
	if err != nil {
		fprintf(options.Stderr, "cfs: remove context %q: %v\n", name, err)
		return exitError
	}
	if _, validateErr := managed.Store.ValidateForWorkspace(managed.Context, managed.Workspace); validateErr != nil {
		combinedErr := errors.Join(validateErr, contextLock.Release())
		if errors.Is(validateErr, os.ErrNotExist) {
			fprintf(options.Stderr, "cfs: context %q does not exist\n", name)
			return exitUnavailable
		}
		fprintf(options.Stderr, "cfs: inspect context %q: %v\n", name, combinedErr)
		return exitError
	}
	destination, moveErr := managed.Store.MoveToTrash(managed.Context)
	releaseErr := contextLock.Release()
	if moveErr != nil {
		fprintf(options.Stderr, "cfs: remove context %q: %v\n", name, moveErr)
		return exitError
	}
	if releaseErr != nil {
		fprintf(options.Stderr, "cfs: %v\n", releaseErr)
		return exitError
	}
	fprintf(options.Stdout, "Moved context %q to %s\n", name, destination)
	fprintf(options.Stdout, "%s\n", trashCredentialWarning)
	return exitOK
}

func managedContextForCommand(options Options, name string) (managedContext, int, bool) {
	cfg, err := config.Load()
	if err != nil {
		return managedContext{}, reportConfigError(options, err), false
	}
	managed, err := resolveManagedContextForName(cfg, name)
	if err != nil {
		reportWorkspaceError(options, err)
		return managedContext{}, exitUnavailable, false
	}
	return managed, exitOK, true
}

func contextItems(stateStore store.Store, ws workspace.Workspace, entries []store.Entry) ([]contextListItem, error) {
	items := make([]contextListItem, 0, len(entries)+1)
	foundDefault := false
	for _, entry := range entries {
		name := entry.Metadata.ContextName
		items = append(items, contextListItem{
			Name: name, Context: shortID(entry.Context.ID), Default: name == contextname.Default, Created: true,
		})
		foundDefault = foundDefault || name == contextname.Default
	}
	if !foundDefault {
		ctx, err := stateStore.ContextFor(ws)
		if err != nil {
			return nil, err
		}
		items = append(items, contextListItem{
			Name: contextname.Default, Context: shortID(ctx.ID), Default: true, Created: false,
		})
	}
	sort.Slice(items, func(first, second int) bool {
		if items[first].Default != items[second].Default {
			return items[first].Default
		}
		return items[first].Name < items[second].Name
	})
	return items, nil
}

func printContextList(output io.Writer, items []contextListItem) {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fprintf(writer, "NAME\tSTATE\tCONTEXT\n")
	for _, item := range items {
		state := "created"
		if !item.Created {
			state = "implicit"
		}
		fprintf(writer, "%s\t%s\t%s\n", item.Name, state, item.Context)
	}
	_ = writer.Flush()
}

func newContextFlagSet(name string, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet("cfs context "+name, flag.ContinueOnError)
	flags.SetOutput(output)
	return flags
}

func printContextHelp(output io.Writer) {
	fprintf(output, "Manage named Cloud Foundry contexts in the current workspace.\n\n")
	fprintf(output, "Usage:\n  cfs context <command> [options]\n\nCommands:\n")
	fprintf(output, "  create <name>          Create an empty context\n")
	fprintf(output, "  list                   List contexts\n")
	fprintf(output, "  status <name>          Show a context and its CF target\n")
	fprintf(output, "  remove <name>          Move a context to recoverable trash\n")
	fprintf(output, "\nRun CF commands with a named context:\n  cfs -c <name> <cf arguments...>\n")
	fprintf(output, "\nNames use 1-63 lowercase letters or digits; '.', '-', and '_' are allowed internally.\n")
}

func printContextSubcommandHelp(output io.Writer, summary, usage string) {
	fprintf(output, "%s\n\nUsage:\n  %s\n", summary, usage)
}
