package app

import (
	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/store"
)

type gcAction string

const (
	gcWouldTrash gcAction = "would-trash"
	gcBusy       gcAction = "busy"
	gcError      gcAction = "error"
	gcTrashed    gcAction = "trashed"
)

type gcEntry struct {
	Context   string   `json:"context"`
	Workspace string   `json:"workspace"`
	Action    gcAction `json:"action"`
	Path      string   `json:"path,omitempty"`
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

	results, failed := collectGarbage(entries, stateStore, *apply)
	if *jsonOutput {
		if code := writeJSON(options, map[string]any{"contexts": results, "applied": *apply}); code != exitOK {
			return code
		}
	} else {
		printGCResults(options, results, *apply)
	}
	if failed {
		return exitError
	}
	return exitOK
}

func collectGarbage(entries []store.Entry, stateStore store.Store, apply bool) ([]gcEntry, bool) {
	results := []gcEntry{}
	failed := false
	for _, entry := range entries {
		if !entry.Orphaned {
			continue
		}
		result := gcEntry{Context: shortID(entry.Context.ID), Workspace: entry.Metadata.Workspace, Action: gcWouldTrash}
		if apply {
			workspaceLock, lockErr := lock.Acquire(entry.Context.LockPath, 0)
			if lockErr != nil {
				result.Action = gcBusy
				failed = true
			} else {
				destination, moveErr := stateStore.MoveToTrash(entry.Context)
				_ = workspaceLock.Release()
				if moveErr != nil {
					result.Action = gcError
					failed = true
				} else {
					result.Action = gcTrashed
					result.Path = destination
				}
			}
		}
		results = append(results, result)
	}
	return results, failed
}

func printGCResults(options Options, results []gcEntry, applied bool) {
	if len(results) == 0 {
		fprintf(options.Stdout, "No orphaned workspace contexts found.\n")
		return
	}
	for _, result := range results {
		fprintf(options.Stdout, "%-12s %s (%s)\n", result.Action, result.Workspace, result.Context)
	}
	if !applied {
		fprintf(options.Stdout, "Run 'cfs gc --apply' to move these contexts to trash.\n")
	}
}
