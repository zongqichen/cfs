package updatecheck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckSelectsHighestPublishedSemanticVersion(t *testing.T) {
	var headersOK atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		headersOK.Store(
			request.Header.Get("Accept") == "application/vnd.github+json" &&
				request.Header.Get("User-Agent") == "cfs-update-check" &&
				request.Header.Get("X-GitHub-Api-Version") == "2022-11-28",
		)
		fprintf(writer, `[
			{"tag_name":"v9.0.0","draft":true},
			{"tag_name":"not-a-version","draft":false},
			{"tag_name":"v0.3.0-rc.1","draft":false,"prerelease":true},
			{"tag_name":"v0.2.1","draft":false}
		]`)
	}))
	defer server.Close()

	service := New(Options{
		Endpoint:  server.URL,
		CachePath: filepath.Join(t.TempDir(), cacheFileName),
	})
	result, err := service.Check(context.Background(), "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if !headersOK.Load() {
		t.Fatal("release request did not include the required GitHub headers")
	}
	if result.Status != StatusUpdateAvailable || !result.UpdateAvailable {
		t.Fatalf("result = %#v, want update available", result)
	}
	if result.CurrentVersion != "0.2.0" || result.LatestVersion != "0.3.0-rc.1" {
		t.Fatalf("result versions = %#v", result)
	}
	if result.ReleaseURL != repositoryReleaseURL+"v0.3.0-rc.1" {
		t.Fatalf("release URL = %q", result.ReleaseURL)
	}
	wantCommand := "go install github.com/zongqichen/cfs/cmd/cfs@v0.3.0-rc.1"
	if len(result.Commands) != 3 || result.Commands[0] != wantCommand {
		t.Fatalf("commands = %q", result.Commands)
	}
}

func TestCompareVersionStates(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		want      Status
		available bool
	}{
		{name: "older release", current: "0.1.0", want: StatusUpdateAvailable, available: true},
		{name: "v prefix", current: "v0.2.0", want: StatusUpToDate},
		{name: "same release", current: "0.2.0", want: StatusUpToDate},
		{name: "newer build", current: "0.3.0", want: StatusAhead},
		{name: "development build", current: "dev", want: StatusDevelopment},
		{name: "dirty build", current: "v0.2.0+dirty", want: StatusDevelopment},
		{name: "unknown build", current: "unknown", want: StatusDevelopment},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := compare(test.current, "v0.2.0")
			if result.Status != test.want || result.UpdateAvailable != test.available {
				t.Fatalf("compare(%q) = %#v", test.current, result)
			}
		})
	}
}

func TestCheckRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http error", status: http.StatusTooManyRequests, body: `{}`, want: "HTTP 429"},
		{name: "invalid json", status: http.StatusOK, body: `{`, want: "decode releases"},
		{name: "no releases", status: http.StatusOK, body: `[{"tag_name":"bad"}]`, want: "no published semantic versions"},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat(" ", responseReadLimit+1), want: "response exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			service := New(Options{Endpoint: server.URL, CachePath: filepath.Join(t.TempDir(), cacheFileName)})
			_, err := service.Check(context.Background(), "0.2.0")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Check() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCheckHonorsHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	service := New(Options{
		Client:    &http.Client{Timeout: 20 * time.Millisecond},
		Endpoint:  server.URL,
		CachePath: filepath.Join(t.TempDir(), cacheFileName),
	})
	if _, err := service.Check(context.Background(), "0.2.0"); err == nil {
		t.Fatal("Check() error = nil, want timeout")
	}
}

func TestNotificationUsesCacheAndNotifiesOncePerVersion(t *testing.T) {
	var requests atomic.Int32
	latest := atomic.Value{}
	latest.Store("v0.2.0")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fprintf(writer, `[{"tag_name":%q}]`, latest.Load().(string))
	}))
	defer server.Close()

	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	cachePath := filepath.Join(t.TempDir(), cacheFileName)
	service := New(Options{
		Endpoint:      server.URL,
		CachePath:     cachePath,
		Now:           func() time.Time { return now },
		CheckInterval: time.Hour,
	})

	result, notify, err := service.Notification(context.Background(), "0.1.0")
	if err != nil || !notify || !result.UpdateAvailable {
		t.Fatalf("first notification = (%#v, %v, %v)", result, notify, err)
	}
	if _, notify, err := service.Notification(context.Background(), "0.1.0"); err != nil || notify {
		t.Fatalf("cached notification = (%v, %v), want no notification", notify, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}

	now = now.Add(2 * time.Hour)
	if _, notify, err := service.Notification(context.Background(), "0.1.0"); err != nil || notify {
		t.Fatalf("same-version refresh = (%v, %v), want no notification", notify, err)
	}
	latest.Store("v0.3.0")
	now = now.Add(2 * time.Hour)
	result, notify, err = service.Notification(context.Background(), "0.1.0")
	if err != nil || !notify || result.LatestVersion != "0.3.0" {
		t.Fatalf("new-version notification = (%#v, %v, %v)", result, notify, err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}

	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "organization", "workspace", "context"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("cache unexpectedly contains %q: %s", forbidden, raw)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("cache permissions = %04o, want 0600", info.Mode().Perm())
		}
	}
}

func TestNotificationCachesNetworkFailure(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("offline")
	})}
	service := New(Options{
		Client:        client,
		Endpoint:      "https://example.invalid/releases",
		CachePath:     filepath.Join(t.TempDir(), cacheFileName),
		Now:           func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) },
		CheckInterval: time.Hour,
	})
	if _, _, err := service.Notification(context.Background(), "0.1.0"); err == nil {
		t.Fatal("first Notification() error = nil, want offline error")
	}
	if _, notify, err := service.Notification(context.Background(), "0.1.0"); err != nil || notify {
		t.Fatalf("cached Notification() = (%v, %v), want silent cache hit", notify, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestNotificationSkipsDevelopmentBuildWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected request")
	})}
	service := New(Options{
		Client:    client,
		Endpoint:  "https://example.invalid/releases",
		CachePath: filepath.Join(t.TempDir(), cacheFileName),
	})

	for _, version := range []string{"dev", "(devel)", "v0.2.0+dirty"} {
		if _, notify, err := service.Notification(context.Background(), version); err != nil || notify {
			t.Fatalf("Notification(%q) = (_, %v, %v), want (_, false, nil)", version, notify, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestConcurrentNotificationsShareOneRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fprintf(writer, `[{"tag_name":"v0.2.0"}]`)
	}))
	defer server.Close()
	service := New(Options{Endpoint: server.URL, CachePath: filepath.Join(t.TempDir(), cacheFileName)})

	var wait sync.WaitGroup
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, _ = service.Notification(context.Background(), "0.1.0")
		}()
	}
	wait.Wait()
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func fprintf(writer http.ResponseWriter, format string, values ...any) {
	_, _ = fmt.Fprintf(writer, format, values...)
}
