package main

import (
	"os"

	"github.com/zongqichen/cfs/internal/app"
)

var version = "dev"
var commit = "unknown"
var buildDate = "unknown"

func main() {
	os.Exit(app.Run(app.Options{
		Args:      os.Args,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}))
}
