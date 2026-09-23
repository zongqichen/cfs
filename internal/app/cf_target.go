package app

import (
	"io"
	"os"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/cfhome"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/envvar"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/runner"
)

func targetAvailable(realCF, home string) (bool, error) {
	targeted, err := cfhome.HasTarget(home)
	if err != nil || !targeted {
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

func globalTargetAvailable() (string, bool, error) {
	home, err := cfhome.Default()
	if err != nil {
		return "", false, err
	}
	available, err := cfhome.HasTarget(home)
	return home, available, err
}

func workspaceConfigMissing(workspaceHome string) (bool, error) {
	exists, err := cfhome.HasConfig(workspaceHome)
	if err != nil {
		return false, err
	}
	return !exists, nil
}
