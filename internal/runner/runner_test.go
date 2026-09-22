package runner

import (
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
