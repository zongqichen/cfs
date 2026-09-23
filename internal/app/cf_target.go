package app

import (
	"io"
	"os"

	"github.com/zongqichen/cfs/internal/cfhome"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/runner"
)

func targetAvailable(realCF, home string) (bool, error) {
	exists, err := cfhome.HasConfig(home)
	if err != nil || !exists {
		return false, err
	}
	if err := cfhome.Validate(home); err != nil {
		return false, err
	}
	env := runner.WithoutEnv(os.Environ(), envvar.CFTrace)
	env = runner.ReplaceEnv(env, map[string]string{envvar.CFHome: home})
	result, err := runner.Run(realCF, []string{"target"}, env, runner.IO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	return err == nil && result.ExitCode == exitOK, err
}

func globalTargetAvailable(realCF string) (string, bool, error) {
	home, err := cfhome.Default()
	if err != nil {
		return "", false, err
	}
	available, err := targetAvailable(realCF, home)
	return home, available, err
}

func workspaceConfigMissing(workspaceHome string) (bool, error) {
	exists, err := cfhome.HasConfig(workspaceHome)
	if err != nil {
		return false, err
	}
	return !exists, nil
}
