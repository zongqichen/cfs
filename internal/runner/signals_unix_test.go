//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runner

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestRunForwardsSIGTERM(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestSignalForwardingHelper")
	command.Env = append(os.Environ(), "CFS_SIGNAL_HELPER=runner")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })

	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		}
	}()
	select {
	case line := <-ready:
		if line != "READY" {
			t.Fatalf("helper output = %q, want READY", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child process")
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("helper error = %v, want exit status", err)
	}
	if exitError.ExitCode() != 23 {
		t.Fatalf("exit code = %d, want forwarded child exit code 23", exitError.ExitCode())
	}
}

func TestSignalForwardingHelper(t *testing.T) {
	switch os.Getenv("CFS_SIGNAL_HELPER") {
	case "runner":
		env := ReplaceEnv(os.Environ(), map[string]string{"CFS_SIGNAL_HELPER": "child"})
		result, err := Run(os.Args[0], []string{"-test.run=TestSignalForwardingHelper"}, env, IO{
			Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(70)
		}
		os.Exit(result.ExitCode)
	case "child":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		fmt.Fprintln(os.Stdout, "READY")
		<-signals
		os.Exit(23)
	}
}
