package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/executable"
	"github.com/zongqichen/cfs/internal/workspace"
)

type checkStatus string

const (
	checkPass checkStatus = "pass"
	checkWarn checkStatus = "warn"
	checkFail checkStatus = "fail"
)

type doctorCheck struct {
	Name    string      `json:"name"`
	Status  checkStatus `json:"status"`
	Message string      `json:"message"`
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
	add := func(name string, status checkStatus, message string) {
		output.Checks = append(output.Checks, doctorCheck{Name: name, Status: status, Message: message})
		if status == checkFail {
			hasFailure = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		add("configuration", checkFail, err.Error())
	} else {
		configPath, _ := config.FilePath()
		add("configuration", checkPass, configPath)
		if err := validateRealCF(cfg.RealCFPath); err != nil {
			add("official-cf", checkFail, err.Error())
		} else {
			add("official-cf", checkPass, cfg.RealCFPath)
		}

		expectedShim := filepath.Join(cfg.ShimDir, executable.Name("cf"))
		if info, statErr := os.Lstat(expectedShim); statErr != nil {
			add("shim", checkFail, statErr.Error())
		} else if info.Mode()&os.ModeSymlink == 0 {
			add("shim", checkFail, "configured shim is not a symbolic link")
		} else {
			target, targetErr := filepath.EvalSymlinks(expectedShim)
			self, selfErr := executable.Current()
			if targetErr != nil || selfErr != nil || !executable.Same(target, self) {
				add("shim", checkFail, "configured shim does not point to this cfs executable")
			} else {
				add("shim", checkPass, expectedShim)
			}
		}

		pathCF, lookErr := exec.LookPath("cf")
		if lookErr != nil {
			add("path", checkFail, "cf is not available on PATH")
		} else if !executable.Same(pathCF, expectedShim) {
			add("path", checkWarn, fmt.Sprintf("cf resolves to %s instead of %s", pathCF, expectedShim))
		} else {
			add("path", checkPass, pathCF)
		}

		if ws, resolveErr := workspace.Resolve(currentDirectory()); resolveErr != nil {
			add("workspace", checkWarn, resolveErr.Error())
		} else {
			add("workspace", checkPass, ws.Root)
		}
	}

	if *jsonOutput {
		if code := writeJSON(options, output); code != exitOK {
			return code
		}
	} else {
		for _, check := range output.Checks {
			fprintf(options.Stdout, "%-5s %-14s %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Message)
		}
	}
	if hasFailure {
		return exitError
	}
	return exitOK
}

func currentDirectory() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
