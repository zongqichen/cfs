package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zongqichen/cfs/internal/envvar"
)

func TestCFInvocationWithoutExecutableSuffixUsesShim(t *testing.T) {
	t.Setenv(envvar.ConfigFile, filepath.Join(t.TempDir(), "missing.json"))
	var stderr bytes.Buffer

	code := Run(Options{Args: []string{"cf", "apps"}, Stderr: &stderr})

	if code != exitUnavailable {
		t.Fatalf("Run() = %d, want %d", code, exitUnavailable)
	}
	if !strings.Contains(stderr.String(), "not configured") {
		t.Fatalf("Run() stderr = %q, want shim configuration error", stderr.String())
	}
}
