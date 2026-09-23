//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

const syntheticRefreshTokenPrefix = "cfs-e2e-refresh-"

type mockTarget struct {
	name              string
	username          string
	password          string
	passcode          string
	promptForPassword bool
	orgName           string
	orgGUID           string
	spaceName         string
	spaceGUID         string
	appName           string
}

var globalMockTarget = mockTarget{
	name:      "global",
	username:  "global-user",
	password:  "global-password",
	orgName:   "global-org",
	orgGUID:   "org-global",
	spaceName: "global-space",
	spaceGUID: "space-global",
	appName:   "global-app",
}

var workspaceMockTargets = []mockTarget{
	{
		name:              "orders",
		username:          "orders-user",
		password:          "orders-password",
		promptForPassword: true,
		orgName:           "commerce",
		orgGUID:           "org-commerce",
		spaceName:         "development",
		spaceGUID:         "space-development",
		appName:           "orders-app",
	},
	{
		name:      "payments",
		username:  "payments-user",
		passcode:  "payments-passcode",
		orgName:   "finance",
		orgGUID:   "org-finance",
		spaceName: "production",
		spaceGUID: "space-production",
		appName:   "payments-app",
	},
	{
		name:      "inventory",
		username:  "inventory-user",
		password:  "inventory-password",
		orgName:   "supply",
		orgGUID:   "org-supply",
		spaceName: "staging",
		spaceGUID: "space-staging",
		appName:   "inventory-app",
	},
	{
		name:      "analytics",
		username:  "analytics-user",
		password:  "analytics-password",
		orgName:   "insights",
		orgGUID:   "org-insights",
		spaceName: "testing",
		spaceGUID: "space-testing",
		appName:   "analytics-app",
	},
	{
		name:      "operations",
		username:  "operations-user",
		password:  "operations-password",
		orgName:   "platform",
		orgGUID:   "org-platform",
		spaceName: "operations",
		spaceGUID: "space-operations",
		appName:   "operations-app",
	},
}

func allMockTargets() []mockTarget {
	targets := make([]mockTarget, 0, len(workspaceMockTargets)+1)
	targets = append(targets, globalMockTarget)
	return append(targets, workspaceMockTargets...)
}

func (target mockTarget) loginCommand(apiURL string) ([]string, string) {
	arguments := []string{
		"login", "-a", apiURL, "--skip-ssl-validation",
		"-o", target.orgName, "-s", target.spaceName,
	}
	if target.passcode != "" {
		return append(arguments, "--sso-passcode", target.passcode), ""
	}
	arguments = append(arguments, "-u", target.username)
	if target.promptForPassword {
		return arguments, target.password + "\n"
	}
	return append(arguments, "-p", target.password), ""
}

func syntheticSecrets() []string {
	secrets := []string{syntheticRefreshTokenPrefix}
	for _, target := range allMockTargets() {
		secrets = append(secrets, target.password, target.passcode)
	}
	return secrets
}

type requestBarrier struct {
	mu       sync.Mutex
	expected int
	arrivals int
	release  chan struct{}
	once     sync.Once
}

func newRequestBarrier(expected int) *requestBarrier {
	return &requestBarrier{expected: expected, release: make(chan struct{})}
}

func (barrier *requestBarrier) wait(ctx context.Context) bool {
	barrier.mu.Lock()
	barrier.arrivals++
	if barrier.arrivals >= barrier.expected {
		barrier.once.Do(func() { close(barrier.release) })
	}
	release := barrier.release
	barrier.mu.Unlock()

	select {
	case <-release:
		return true
	case <-time.After(requestBarrierTimeout):
		return false
	case <-ctx.Done():
		return false
	}
}

func (barrier *requestBarrier) count() int {
	barrier.mu.Lock()
	defer barrier.mu.Unlock()
	return barrier.arrivals
}

func (barrier *requestBarrier) unblock() {
	if barrier != nil {
		barrier.once.Do(func() { close(barrier.release) })
	}
}

type mockCF struct {
	server *httptest.Server

	mu           sync.Mutex
	issuedTokens map[string]string
	tokenBarrier *requestBarrier
	appBarrier   *requestBarrier

	blockStarted chan struct{}
	blockRelease chan struct{}
	startOnce    sync.Once
	blockOnce    sync.Once
}

func newMockCF() *mockCF {
	mock := &mockCF{
		issuedTokens: make(map[string]string),
		blockStarted: make(chan struct{}),
		blockRelease: make(chan struct{}),
	}
	mock.server = httptest.NewUnstartedServer(http.HandlerFunc(mock.serveHTTP))
	mock.server.StartTLS()
	return mock
}

func (m *mockCF) close() {
	m.unblock()
	m.mu.Lock()
	tokenBarrier := m.tokenBarrier
	appBarrier := m.appBarrier
	m.mu.Unlock()
	if tokenBarrier != nil {
		tokenBarrier.unblock()
	}
	if appBarrier != nil {
		appBarrier.unblock()
	}
	m.server.Close()
}

func (m *mockCF) expectConcurrentTokenRequests(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenBarrier = newRequestBarrier(count)
}

func (m *mockCF) expectConcurrentAppRequests(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appBarrier = newRequestBarrier(count)
}

func (m *mockCF) tokenRequestCount() int {
	m.mu.Lock()
	barrier := m.tokenBarrier
	m.mu.Unlock()
	if barrier == nil {
		return 0
	}
	return barrier.count()
}

func (m *mockCF) appRequestCount() int {
	m.mu.Lock()
	barrier := m.appBarrier
	m.mu.Unlock()
	if barrier == nil {
		return 0
	}
	return barrier.count()
}

func (m *mockCF) unblock() {
	m.blockOnce.Do(func() { close(m.blockRelease) })
}

func (m *mockCF) serveHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/":
		m.serveRoot(response)
	case "/v2/info":
		m.writeJSON(response, http.StatusOK, map[string]any{
			"api_version":            "2.240.0",
			"authorization_endpoint": m.server.URL,
		})
	case "/v3/info":
		m.writeJSON(response, http.StatusOK, map[string]any{
			"name": "cfs-e2e", "build": "test", "osbapi_version": "2.17",
		})
	case "/login":
		m.writeJSON(response, http.StatusOK, map[string]any{
			"prompts": map[string]any{
				"username": []string{"text", "Email"},
				"password": []string{"password", "Password"},
				"passcode": []string{"password", "One-time passcode"},
			},
		})
	case "/oauth/token":
		m.serveToken(response, request)
	case "/v3/organizations":
		m.serveOrganizations(response, request)
	case "/v3/spaces":
		m.serveSpaces(response, request)
	case "/v3/apps":
		m.serveApps(response, request)
	case "/v3/routes":
		if _, ok := m.authorized(response, request); ok {
			m.writeList(response, nil)
		}
	case "/e2e/block":
		if _, ok := m.authorized(response, request); ok {
			m.waitUntilReleased(response, request)
		}
	default:
		m.writeJSON(response, http.StatusNotFound, map[string]any{
			"errors": []map[string]string{{"code": "CF-NotFound", "title": "Not Found", "detail": "unknown mock route"}},
		})
	}
}

func (m *mockCF) serveRoot(response http.ResponseWriter) {
	links := map[string]any{
		"self":                apiLink(m.server.URL, ""),
		"cloud_controller_v2": apiLink(m.server.URL+"/v2", "2.240.0"),
		"cloud_controller_v3": apiLink(m.server.URL+"/v3", "3.225.0"),
		"network_policy_v1":   apiLink(m.server.URL+"/networking/v1/external", ""),
		"uaa":                 apiLink(m.server.URL, ""),
		"login":               apiLink(m.server.URL, ""),
		"logging":             apiLink("wss://unused.invalid", ""),
		"log_cache":           apiLink(m.server.URL, ""),
		"app_ssh": map[string]any{
			"href": "unused.invalid:2222",
			"meta": map[string]string{"host_key_fingerprint": "unused", "oauth_client": "ssh-proxy"},
		},
	}
	m.writeJSON(response, http.StatusOK, map[string]any{"links": links})
}

func apiLink(href, version string) map[string]any {
	link := map[string]any{"href": href}
	if version != "" {
		link["meta"] = map[string]string{"version": version}
	}
	return link
}

func (m *mockCF) serveToken(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	if err := request.ParseForm(); err != nil {
		m.writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	client, secret, basicOK := request.BasicAuth()
	if !basicOK || client != "cf" || secret != "" {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	if request.Form.Get("grant_type") != "password" {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_grant"})
		return
	}

	identity := m.authenticate(request.Form)
	if identity == "" {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_grant"})
		return
	}
	if barrier := m.currentTokenBarrier(); barrier != nil && !barrier.wait(request.Context()) {
		m.writeJSON(response, http.StatusGatewayTimeout, map[string]string{"error": "token barrier timed out"})
		return
	}

	token := unsignedToken(identity, time.Now().Add(time.Hour))
	m.mu.Lock()
	m.issuedTokens[token] = identity
	m.mu.Unlock()
	m.writeJSON(response, http.StatusOK, map[string]any{
		"access_token":  token,
		"refresh_token": syntheticRefreshTokenPrefix + identity,
		"token_type":    "bearer",
		"expires_in":    3600,
		"scope":         "openid cloud_controller.read cloud_controller.write",
	})
}

func (m *mockCF) authenticate(form url.Values) string {
	for _, target := range allMockTargets() {
		passwordMatch := target.password != "" && form.Get("username") == target.username && form.Get("password") == target.password
		passcodeMatch := target.passcode != "" && form.Get("passcode") == target.passcode
		if passwordMatch || passcodeMatch {
			return target.username
		}
	}
	return ""
}

func unsignedToken(identity string, expiration time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte("{\"alg\":\"none\",\"typ\":\"JWT\"}"))
	claims, err := json.Marshal(map[string]any{
		"exp": expiration.Unix(), "user_name": identity, "user_id": "user-" + identity, "origin": "uaa",
	})
	if err != nil {
		panic(fmt.Sprintf("encode synthetic access token: %v", err))
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + "."
}

func (m *mockCF) authorized(response http.ResponseWriter, request *http.Request) (string, bool) {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
		return "", false
	}
	token := strings.TrimSpace(header[len("bearer "):])
	m.mu.Lock()
	identity, found := m.issuedTokens[token]
	m.mu.Unlock()
	if !found {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unknown bearer token"})
		return "", false
	}
	return identity, true
}

func (m *mockCF) serveOrganizations(response http.ResponseWriter, request *http.Request) {
	identity, ok := m.authorized(response, request)
	if !ok {
		return
	}
	name := request.URL.Query().Get("names")
	resources := make([]map[string]any, 0, 1)
	for _, target := range allMockTargets() {
		if target.username == identity && (name == "" || target.orgName == name) {
			resources = append(resources, map[string]any{"guid": target.orgGUID, "name": target.orgName})
		}
	}
	m.writeList(response, resources)
}

func (m *mockCF) serveSpaces(response http.ResponseWriter, request *http.Request) {
	identity, ok := m.authorized(response, request)
	if !ok {
		return
	}
	name := request.URL.Query().Get("names")
	orgGUID := request.URL.Query().Get("organization_guids")
	resources := make([]map[string]any, 0, 1)
	for _, target := range allMockTargets() {
		if target.username == identity && (name == "" || target.spaceName == name) && (orgGUID == "" || target.orgGUID == orgGUID) {
			resources = append(resources, map[string]any{
				"guid": target.spaceGUID, "name": target.spaceName,
				"relationships": map[string]any{"organization": map[string]any{"data": map[string]string{"guid": target.orgGUID}}},
			})
		}
	}
	m.writeList(response, resources)
}

func (m *mockCF) serveApps(response http.ResponseWriter, request *http.Request) {
	identity, ok := m.authorized(response, request)
	if !ok {
		return
	}
	spaceGUID := request.URL.Query().Get("space_guids")
	for _, target := range allMockTargets() {
		if target.spaceGUID != spaceGUID {
			continue
		}
		if target.username != identity {
			m.writeJSON(response, http.StatusForbidden, map[string]string{"error": "identity cannot access target space"})
			return
		}
		if barrier := m.currentAppBarrier(); barrier != nil && !barrier.wait(request.Context()) {
			m.writeJSON(response, http.StatusGatewayTimeout, map[string]string{"error": "app barrier timed out"})
			return
		}
		m.writeList(response, []map[string]any{{
			"guid": "app-" + target.spaceGUID, "name": target.appName, "state": "STARTED",
			"relationships": map[string]any{"space": map[string]any{"data": map[string]string{"guid": target.spaceGUID}}},
		}})
		return
	}
	m.writeList(response, nil)
}

func (m *mockCF) currentTokenBarrier() *requestBarrier {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokenBarrier
}

func (m *mockCF) currentAppBarrier() *requestBarrier {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appBarrier
}

func (m *mockCF) waitUntilReleased(response http.ResponseWriter, request *http.Request) {
	m.startOnce.Do(func() { close(m.blockStarted) })
	select {
	case <-m.blockRelease:
		m.writeJSON(response, http.StatusOK, map[string]bool{"released": true})
	case <-request.Context().Done():
	}
}

func (m *mockCF) writeList(response http.ResponseWriter, resources []map[string]any) {
	m.writeJSON(response, http.StatusOK, map[string]any{
		"pagination": map[string]any{
			"total_results": len(resources), "total_pages": 1, "first": nil, "last": nil, "next": nil, "previous": nil,
		},
		"resources": resources,
	})
}

func (m *mockCF) writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		panic(fmt.Sprintf("encode mock response: %v", err))
	}
}
