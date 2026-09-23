package runner

import (
	"runtime"
	"strings"
	"testing"
)

func TestReplaceEnvReplacesAndAddsValues(t *testing.T) {
	got := ReplaceEnv([]string{"A=old", "B=keep"}, map[string]string{
		"A": "new",
		"C": "added",
	})
	joined := strings.Join(got, "\n")
	for _, expected := range []string{"A=new", "B=keep", "C=added"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("ReplaceEnv() = %v, missing %q", got, expected)
		}
	}
	if strings.Contains(joined, "A=old") {
		t.Fatalf("ReplaceEnv() retained old value: %v", got)
	}
}

func TestWithoutEnvRemovesOnlyNamedValues(t *testing.T) {
	got := WithoutEnv([]string{"CF_TRACE=/private/trace", "CF_HOME=/keep", "FLAG"}, "CF_TRACE")
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "CF_TRACE") {
		t.Fatalf("WithoutEnv() retained CF_TRACE: %v", got)
	}
	for _, expected := range []string{"CF_HOME=/keep", "FLAG"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("WithoutEnv() = %v, missing %q", got, expected)
		}
	}
}

func TestEnvironmentNamesAreCaseInsensitiveOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("environment names are case-sensitive on this platform")
	}

	replaced := ReplaceEnv([]string{"cf_home=old", "PATH=keep"}, map[string]string{"CF_HOME": "new"})
	joined := strings.Join(replaced, "\n")
	if strings.Contains(joined, "cf_home=old") || !strings.Contains(joined, "CF_HOME=new") {
		t.Fatalf("ReplaceEnv() = %v", replaced)
	}

	removed := WithoutEnv([]string{"cf_trace=secret", "PATH=keep"}, "CF_TRACE")
	if strings.Contains(strings.Join(removed, "\n"), "cf_trace") {
		t.Fatalf("WithoutEnv() = %v", removed)
	}
}
