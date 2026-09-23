package app

import (
	"strings"
	"testing"
)

func TestParseNamedInvocation(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantName  string
		wantArgs  []string
		wantNamed bool
		wantError string
	}{
		{name: "short flag", args: []string{"-c", "prod", "apps"}, wantName: "prod", wantArgs: []string{"apps"}, wantNamed: true},
		{name: "long flag", args: []string{"--context", "poc", "target"}, wantName: "poc", wantArgs: []string{"target"}, wantNamed: true},
		{name: "long equals flag", args: []string{"--context=dev", "logs", "api"}, wantName: "dev", wantArgs: []string{"logs", "api"}, wantNamed: true},
		{name: "control command", args: []string{"context", "list"}},
		{name: "missing name", args: []string{"-c"}, wantNamed: true, wantError: "requires a name"},
		{name: "missing command", args: []string{"-c", "prod"}, wantNamed: true, wantError: "requires a CF command"},
		{name: "invalid name", args: []string{"-c", "Prod", "apps"}, wantNamed: true, wantError: "invalid context name"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, args, named, err := parseNamedInvocation(test.args)
			if name != test.wantName || strings.Join(args, " ") != strings.Join(test.wantArgs, " ") || named != test.wantNamed {
				t.Fatalf("parseNamedInvocation() = %q, %q, %t; want %q, %q, %t", name, args, named, test.wantName, test.wantArgs, test.wantNamed)
			}
			if test.wantError == "" && err != nil {
				t.Fatalf("parseNamedInvocation() error = %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("parseNamedInvocation() error = %v, want %q", err, test.wantError)
			}
		})
	}
}
