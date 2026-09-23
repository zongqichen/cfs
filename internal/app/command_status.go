package app

import (
	"bytes"
	"os"
	"strings"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/runner"
)

type statusOutput struct {
	Mode           string `json:"mode"`
	Workspace      string `json:"workspace,omitempty"`
	Source         string `json:"source"`
	Context        string `json:"context,omitempty"`
	OfficialCF     string `json:"official_cf,omitempty"`
	CFHome         string `json:"cf_home,omitempty"`
	TargetOutput   string `json:"target_output,omitempty"`
	TargetExitCode int    `json:"target_exit_code"`
	Redacted       bool   `json:"redacted,omitempty"`
}

func commandStatus(options Options, args []string) int {
	flags := newFlagSet("status", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	redact := flags.Bool("redact", false, "omit paths and CF target details")
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

	status := statusOutput{OfficialCF: cfg.RealCFPath, Redacted: *redact}
	env := os.Environ()
	if home, external := externalCFHome(); external {
		status.Mode = "external"
		status.Source = envvar.CFHome
		status.CFHome = home
	} else {
		managed, resolveErr := resolveManagedContext(cfg)
		if resolveErr != nil {
			fprintf(options.Stderr, "cfs: %v\n", resolveErr)
			return exitUnavailable
		}
		timeout, timeoutErr := lockTimeout()
		if timeoutErr != nil {
			fprintf(options.Stderr, "cfs: %v\n", timeoutErr)
			return exitUsage
		}
		workspaceLock, activateErr := managed.activate(timeout)
		if activateErr != nil {
			fprintf(options.Stderr, "cfs: cannot inspect workspace: %v\n", activateErr)
			return exitTemporary
		}
		defer workspaceLock.Release()

		status.Mode = "managed"
		status.Workspace = managed.Workspace.Root
		status.Source = managed.Workspace.Source
		status.Context = shortID(managed.Context.ID)
		status.CFHome = managed.Context.CFHome
		env = managed.environment(env)
	}

	var targetStdout bytes.Buffer
	var targetStderr bytes.Buffer
	env = runner.WithoutEnv(env, envvar.CFTrace)
	result, runErr := runner.Run(cfg.RealCFPath, []string{"target"}, env, runner.IO{
		Stdin: options.Stdin, Stdout: &targetStdout, Stderr: &targetStderr,
	})
	if runErr != nil {
		fprintf(options.Stderr, "cfs: inspect CF target: %v\n", runErr)
		return exitUnavailable
	}
	status.TargetExitCode = result.ExitCode
	if *redact {
		status.Workspace = ""
		status.OfficialCF = ""
		status.CFHome = ""
	} else {
		status.TargetOutput = joinNonEmpty(targetStdout.String(), targetStderr.String())
	}

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
	if *redact {
		fprintf(options.Stdout, "CF target exit code: %d\n", status.TargetExitCode)
		fprintf(options.Stdout, "Details: redacted\n")
		return result.ExitCode
	}
	fprintf(options.Stdout, "CF CLI: %s\n", status.OfficialCF)
	fprintf(options.Stdout, "CF home: %s\n", status.CFHome)
	fprintf(options.Stdout, "CF target:\n%s\n", status.TargetOutput)
	return result.ExitCode
}

func joinNonEmpty(values ...string) string {
	nonEmpty := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return strings.Join(nonEmpty, "\n")
}
