package pathutil

import (
	"runtime"
	"testing"
)

func TestEqualUsesPlatformPathCaseRules(t *testing.T) {
	got := Equal("Example", "example")
	want := runtime.GOOS == "windows"
	if got != want {
		t.Fatalf("Equal() = %t, want %t on %s", got, want, runtime.GOOS)
	}
}
