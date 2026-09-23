//go:build e2e

package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

func requireSuccess(t *testing.T, operation string, result commandResult) {
	t.Helper()
	if result.code != 0 {
		t.Fatalf("%s failed: code=%d stdout=%q stderr=%q", operation, result.code, result.stdout, result.stderr)
	}
}

func assertContains(t *testing.T, value, substring, message string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("%s: output=%q", message, value)
	}
}

func configSnapshot(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test configuration: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse test configuration: %v", err)
	}
	keys := []string{
		"AccessToken", "APIVersion", "AuthorizationEndpoint", "OrganizationFields",
		"RefreshToken", "SpaceFields", "SSLDisabled", "Target", "UaaEndpoint",
		"UAAGrantType", "UAAOAuthClient", "UAAOAuthClientSecret",
	}
	snapshot := make(map[string]string, len(keys))
	for _, key := range keys {
		value := document[key]
		canonical, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("canonicalize test configuration field: %v", err)
		}
		sum := sha256.Sum256(canonical)
		snapshot[key] = hex.EncodeToString(sum[:])
	}
	return snapshot
}

func rawFileDigest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test file for digest: %v", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func assertRawDigest(t *testing.T, path, expected, message string) {
	t.Helper()
	if actual := rawFileDigest(t, path); actual != expected {
		t.Fatal(message)
	}
}

func assertConfigSnapshot(t *testing.T, path string, expected map[string]string, message string) {
	t.Helper()
	actual := configSnapshot(t, path)
	var changed []string
	for key, expectedDigest := range expected {
		if actual[key] != expectedDigest {
			changed = append(changed, key)
		}
	}
	for key := range actual {
		if _, found := expected[key]; !found {
			changed = append(changed, key)
		}
	}
	if len(changed) > 0 {
		sort.Strings(changed)
		t.Fatalf("%s (changed fields: %s)", message, strings.Join(changed, ", "))
	}
}

func assertJSONField(t *testing.T, raw, field string, expected any) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse JSON output: %v", err)
	}
	if !reflect.DeepEqual(value[field], expected) {
		t.Fatalf("JSON field %s=%v, want %v", field, value[field], expected)
	}
}

type doctorOutput struct {
	Checks []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"checks"`
}

func parseDoctorOutput(t *testing.T, raw string) doctorOutput {
	t.Helper()
	var output doctorOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		t.Fatalf("parse doctor output: %v", err)
	}
	if len(output.Checks) == 0 {
		t.Fatal("doctor output contains no checks")
	}
	return output
}

func assertDoctorHasNoFailure(t *testing.T, raw string) {
	t.Helper()
	for _, check := range parseDoctorOutput(t, raw).Checks {
		switch check.Status {
		case "pass", "warn":
		case "fail":
			t.Fatalf("doctor check failed: %s", check.Name)
		default:
			t.Fatalf("doctor check %s has unknown status %q", check.Name, check.Status)
		}
	}
}

func assertDoctorCheck(t *testing.T, raw, name, status string) {
	t.Helper()
	for _, check := range parseDoctorOutput(t, raw).Checks {
		if check.Name == name {
			if check.Status != status {
				t.Fatalf("doctor check %s=%s, want %s", name, check.Status, status)
			}
			return
		}
	}
	t.Fatalf("doctor output is missing check %s", name)
}

func assertGCAction(t *testing.T, raw, action string) {
	t.Helper()
	var value struct {
		Contexts []struct {
			Action string
		}
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse gc output: %v", err)
	}
	for _, context := range value.Contexts {
		if context.Action == action {
			return
		}
	}
	t.Fatalf("gc output is missing action %s", action)
}

func assertRedactedStatus(t *testing.T, raw string, forbidden []string) {
	t.Helper()
	assertJSONField(t, raw, "redacted", true)
	for _, value := range forbidden {
		if strings.Contains(raw, value) {
			t.Fatal("redacted status exposed forbidden operational metadata")
		}
	}
}

func contextFromStatus(t *testing.T, raw string) string {
	t.Helper()
	var value struct {
		Context string
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse status output: %v", err)
	}
	if value.Context == "" {
		t.Fatal("status did not include a context ID")
	}
	return value.Context
}

func assertMetadataHasNoSecrets(t *testing.T, stateRoot string, forbidden []string) {
	t.Helper()
	inspected := 0
	err := filepath.WalkDir(stateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "metadata.json" {
			return nil
		}
		inspected++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range forbidden {
			if secret == "" {
				continue
			}
			if bytes.Contains(raw, []byte(secret)) {
				return errors.New("cfs metadata contains synthetic credentials")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect cfs metadata: %v", err)
	}
	if inspected == 0 {
		t.Fatal("no cfs metadata was available for the secret check")
	}
}

func directorySnapshot(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot state directory: %v", err)
	}
	sort.Strings(paths)
	return paths
}

func assertEqualStrings(t *testing.T, actual, expected []string, message string) {
	t.Helper()
	if !slices.Equal(actual, expected) {
		t.Fatalf("%s: got %v, want %v", message, actual, expected)
	}
}
