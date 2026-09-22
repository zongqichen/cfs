package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
)

type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Result struct {
	ExitCode int
}

func Run(path string, args []string, env []string, streams IO) (Result, error) {
	command := exec.Command(path, args...)
	command.Env = env
	command.Stdin = streams.Stdin
	command.Stdout = streams.Stdout
	command.Stderr = streams.Stderr

	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("start official CF CLI: %w", err)
	}

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, forwardedSignals()...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range signals {
			_ = command.Process.Signal(sig)
		}
	}()

	err := command.Wait()
	signal.Stop(signals)
	close(signals)
	<-done

	if err == nil {
		return Result{ExitCode: 0}, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return Result{ExitCode: exitError.ExitCode()}, nil
	}
	return Result{}, fmt.Errorf("wait for official CF CLI: %w", err)
}

func ReplaceEnv(env []string, values map[string]string) []string {
	result := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		key := entry
		for i, character := range entry {
			if character == '=' {
				key = entry[:i]
				break
			}
		}
		if _, replaced := values[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}
