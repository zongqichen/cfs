package app

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

func confirm(options Options, command, prompt string) (bool, int) {
	if !isTerminal(options.Stdin) {
		fprintf(options.Stderr, "cfs: %s requires --yes when standard input is not a terminal\n", command)
		return false, exitUsage
	}
	fprintf(options.Stdout, "%s [y/N] ", prompt)
	answer, err := bufio.NewReader(options.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fprintf(options.Stderr, "cfs: read confirmation: %v\n", err)
		return false, exitError
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fprintf(options.Stdout, "Cancelled.\n")
		return false, exitOK
	}
	return true, exitOK
}

func isTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
