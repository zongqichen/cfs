package pathutil

import (
	"path/filepath"
	"runtime"
	"strings"
)

func Equal(first, second string) bool {
	first = absoluteClean(first)
	second = absoluteClean(second)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(first, second)
	}
	return first == second
}

func Identity(path string) string {
	clean := absoluteClean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func absoluteClean(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}
