package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/lock"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/securefs"
	"golang.org/x/mod/semver"
)

const (
	DefaultEndpoint      = "https://api.github.com/repos/zongqichen/cloud-foundry-cli-contexts/releases?per_page=100"
	DefaultCheckInterval = 24 * time.Hour
	DefaultHTTPTimeout   = 3 * time.Second

	cacheVersion         = 1
	cacheFileName        = "update-check.json"
	cacheReadLimit       = 64 * 1024
	responseReadLimit    = 1024 * 1024
	repositoryReleaseURL = "https://github.com/zongqichen/cloud-foundry-cli-contexts/releases/tag/"
	goInstallPackage     = "github.com/zongqichen/cloud-foundry-cli-contexts/cmd/cfs"
)

type Status string

const (
	StatusUpdateAvailable Status = "update-available"
	StatusUpToDate        Status = "up-to-date"
	StatusAhead           Status = "ahead"
	StatusDevelopment     Status = "development"
)

type Result struct {
	CurrentVersion  string   `json:"current_version"`
	LatestVersion   string   `json:"latest_version"`
	UpdateAvailable bool     `json:"update_available"`
	Status          Status   `json:"status"`
	ReleaseURL      string   `json:"release_url"`
	Commands        []string `json:"commands"`
}

type Options struct {
	Client        *http.Client
	Endpoint      string
	CachePath     string
	Now           func() time.Time
	CheckInterval time.Duration
}

type Service struct {
	client        *http.Client
	endpoint      string
	cachePath     string
	now           func() time.Time
	checkInterval time.Duration
}

type release struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
}

type cache struct {
	Version         int       `json:"version"`
	CheckedAt       time.Time `json:"checked_at"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	NotifiedVersion string    `json:"notified_version,omitempty"`
}

func New(options Options) *Service {
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	interval := options.CheckInterval
	if interval <= 0 {
		interval = DefaultCheckInterval
	}
	cachePath := options.CachePath
	if cachePath == "" {
		cachePath = defaultCachePath()
	}
	return &Service{
		client:        client,
		endpoint:      endpoint,
		cachePath:     cachePath,
		now:           now,
		checkInterval: interval,
	}
}

func (service *Service) Check(ctx context.Context, currentVersion string) (Result, error) {
	latestVersion, err := service.fetchLatest(ctx)
	if err != nil {
		return Result{}, err
	}
	result := compare(currentVersion, latestVersion)
	service.remember(result, true)
	return result, nil
}

func (service *Service) Notification(ctx context.Context, currentVersion string) (Result, bool, error) {
	if canonicalVersion(currentVersion) == "" || service.cachePath == "" {
		return Result{}, false, nil
	}
	if err := securefs.EnsureDirectory(filepath.Dir(service.cachePath)); err != nil {
		return Result{}, false, err
	}
	cacheLock, err := lock.Acquire(service.cachePath+".lock", 0)
	if errors.Is(err, lock.ErrBusy) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	defer cacheLock.Release()

	stored, _ := readCache(service.cachePath)
	now := service.now().UTC()
	if stored.CheckedAt.IsZero() || now.Before(stored.CheckedAt) || now.Sub(stored.CheckedAt) >= service.checkInterval {
		latestVersion, fetchErr := service.fetchLatest(ctx)
		stored.CheckedAt = now
		if fetchErr != nil {
			_ = writeCache(service.cachePath, stored)
			return Result{}, false, fetchErr
		}
		if stored.LatestVersion != latestVersion {
			stored.NotifiedVersion = ""
		}
		stored.LatestVersion = latestVersion
	}
	if stored.LatestVersion == "" {
		_ = writeCache(service.cachePath, stored)
		return Result{}, false, nil
	}

	result := compare(currentVersion, stored.LatestVersion)
	notify := result.UpdateAvailable && stored.NotifiedVersion != stored.LatestVersion
	if notify {
		stored.NotifiedVersion = stored.LatestVersion
	}
	if err := writeCache(service.cachePath, stored); err != nil {
		return Result{}, false, err
	}
	return result, notify, nil
}

func (service *Service) fetchLatest(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "cfs-update-check")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := service.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("request releases: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub Releases returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseReadLimit+1))
	if err != nil {
		return "", fmt.Errorf("read releases: %w", err)
	}
	if len(raw) > responseReadLimit {
		return "", fmt.Errorf("GitHub Releases response exceeds %d bytes", responseReadLimit)
	}

	var releases []release
	if err := json.Unmarshal(raw, &releases); err != nil {
		return "", fmt.Errorf("decode releases: %w", err)
	}
	latest := ""
	for _, candidate := range releases {
		version := canonicalVersion(candidate.TagName)
		if candidate.Draft || version == "" {
			continue
		}
		if latest == "" || semver.Compare(version, latest) > 0 {
			latest = version
		}
	}
	if latest == "" {
		return "", errors.New("GitHub Releases returned no published semantic versions")
	}
	return latest, nil
}

func compare(currentVersion, latestVersion string) Result {
	current := canonicalVersion(currentVersion)
	latest := canonicalVersion(latestVersion)
	result := Result{
		CurrentVersion: displayVersion(currentVersion),
		LatestVersion:  displayVersion(latest),
		Status:         StatusDevelopment,
		ReleaseURL:     releaseURL(latest),
		Commands:       []string{},
	}
	if current == "" {
		return result
	}
	switch semver.Compare(current, latest) {
	case -1:
		result.Status = StatusUpdateAvailable
		result.UpdateAvailable = true
		result.Commands = []string{
			"go install " + goInstallPackage + "@" + latest,
			"cfs setup",
			"cfs doctor",
		}
	case 0:
		result.Status = StatusUpToDate
	default:
		result.Status = StatusAhead
	}
	return result
}

func canonicalVersion(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "+dirty") {
		return ""
	}
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	if !semver.IsValid(value) {
		return ""
	}
	return semver.Canonical(value)
}

func displayVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.TrimPrefix(value, "v")
}

func releaseURL(version string) string {
	if version == "" {
		return ""
	}
	return repositoryReleaseURL + url.PathEscape(version)
}

func defaultCachePath() string {
	root, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "cfs", cacheFileName)
}

func (service *Service) remember(result Result, notified bool) {
	if service.cachePath == "" {
		return
	}
	stored := cache{
		Version:       cacheVersion,
		CheckedAt:     service.now().UTC(),
		LatestVersion: canonicalVersion(result.LatestVersion),
	}
	if notified && result.UpdateAvailable {
		stored.NotifiedVersion = stored.LatestVersion
	}
	_ = writeCache(service.cachePath, stored)
}

func readCache(path string) (cache, error) {
	file, err := securefs.OpenRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache{Version: cacheVersion}, nil
	}
	if err != nil {
		return cache{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, cacheReadLimit+1))
	if err != nil {
		return cache{}, err
	}
	if len(raw) > cacheReadLimit {
		return cache{}, fmt.Errorf("update cache exceeds %d bytes", cacheReadLimit)
	}
	var stored cache
	if err := json.Unmarshal(raw, &stored); err != nil {
		return cache{}, err
	}
	if stored.Version != cacheVersion {
		return cache{}, fmt.Errorf("unsupported update cache version %d", stored.Version)
	}
	return stored, nil
}

func writeCache(path string, stored cache) error {
	stored.Version = cacheVersion
	return securefs.WriteJSONAtomic(path, stored)
}
