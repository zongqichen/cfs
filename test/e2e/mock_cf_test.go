//go:build e2e && !windows

package e2e

import (
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

const (
	testUsername = "cfs-e2e-user"
	testPassword = "cfs-e2e-password"
	testPasscode = "cfs-e2e-passcode"
)

type mockTarget struct {
	orgName   string
	orgGUID   string
	spaceName string
	spaceGUID string
	appName   string
}

var mockTargets = []mockTarget{
	{orgName: "global-org", orgGUID: "org-global", spaceName: "global-space", spaceGUID: "space-global", appName: "global-app"},
	{orgName: "commerce", orgGUID: "org-commerce", spaceName: "development", spaceGUID: "space-development", appName: "orders-app"},
	{orgName: "finance", orgGUID: "org-finance", spaceName: "production", spaceGUID: "space-production", appName: "payments-app"},
}

type recordedRequest struct {
	method string
	path   string
	query  url.Values
}

type mockCF struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []recordedRequest

	blockStarted chan struct{}
	blockRelease chan struct{}
	startOnce    sync.Once
	blockOnce    sync.Once

	barrierArrivals int
	barrierRelease  chan struct{}
	barrierOnce     sync.Once
}

func newMockCF() *mockCF {
	mock := &mockCF{
		blockStarted:   make(chan struct{}),
		blockRelease:   make(chan struct{}),
		barrierRelease: make(chan struct{}),
	}
	mock.server = httptest.NewTLSServer(http.HandlerFunc(mock.serveHTTP))
	return mock
}

func (m *mockCF) close() {
	m.unblock()
	m.barrierOnce.Do(func() { close(m.barrierRelease) })
	m.server.Close()
}

func (m *mockCF) unblock() {
	m.blockOnce.Do(func() { close(m.blockRelease) })
}

func (m *mockCF) serveHTTP(response http.ResponseWriter, request *http.Request) {
	m.record(request)

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
		if m.authorized(response, request) {
			m.serveOrganizations(response, request)
		}
	case "/v3/spaces":
		if m.authorized(response, request) {
			m.serveSpaces(response, request)
		}
	case "/v3/apps":
		if m.authorized(response, request) {
			m.serveApps(response, request)
		}
	case "/v3/routes":
		if m.authorized(response, request) {
			m.writeList(response, nil)
		}
	case "/e2e/block":
		if m.authorized(response, request) {
			m.waitUntilReleased(response, request)
		}
	case "/e2e/barrier":
		if m.authorized(response, request) {
			m.waitAtBarrier(response, request)
		}
	default:
		m.writeJSON(response, http.StatusNotFound, map[string]any{
			"errors": []map[string]string{{"code": "CF-NotFound", "title": "Not Found", "detail": "unknown mock route"}},
		})
	}
}

func (m *mockCF) record(request *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, recordedRequest{
		method: request.Method,
		path:   request.URL.Path,
		query:  request.URL.Query(),
	})
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
	client, secret, basicOK := request.BasicAuth()
	if request.Method != http.MethodPost || request.ParseForm() != nil || !basicOK || client != "cf" || secret != "" {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	passwordLogin := request.Form.Get("username") == testUsername && request.Form.Get("password") == testPassword
	passcodeLogin := request.Form.Get("passcode") == testPasscode
	if request.Form.Get("grant_type") != "password" || (!passwordLogin && !passcodeLogin) {
		m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_grant"})
		return
	}
	m.writeJSON(response, http.StatusOK, map[string]any{
		"access_token":  unsignedToken(time.Now().Add(time.Hour)),
		"refresh_token": "synthetic-refresh-token",
		"token_type":    "bearer",
		"expires_in":    3600,
		"scope":         "openid cloud_controller.read cloud_controller.write",
	})
}

func unsignedToken(expiration time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte("{\"alg\":\"none\",\"typ\":\"JWT\"}"))
	claims, _ := json.Marshal(map[string]any{
		"exp": expiration.Unix(), "user_name": testUsername, "user_id": "user-e2e", "origin": "uaa",
	})
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + "."
}

func (m *mockCF) authorized(response http.ResponseWriter, request *http.Request) bool {
	if strings.HasPrefix(strings.ToLower(request.Header.Get("Authorization")), "bearer ") {
		return true
	}
	m.writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
	return false
}

func (m *mockCF) serveOrganizations(response http.ResponseWriter, request *http.Request) {
	name := request.URL.Query().Get("names")
	resources := make([]map[string]any, 0, len(mockTargets))
	for _, target := range mockTargets {
		if name == "" || target.orgName == name {
			resources = append(resources, map[string]any{"guid": target.orgGUID, "name": target.orgName})
		}
	}
	m.writeList(response, resources)
}

func (m *mockCF) serveSpaces(response http.ResponseWriter, request *http.Request) {
	name := request.URL.Query().Get("names")
	orgGUID := request.URL.Query().Get("organization_guids")
	resources := make([]map[string]any, 0, len(mockTargets))
	for _, target := range mockTargets {
		if (name == "" || target.spaceName == name) && (orgGUID == "" || target.orgGUID == orgGUID) {
			resources = append(resources, map[string]any{
				"guid": target.spaceGUID, "name": target.spaceName,
				"relationships": map[string]any{"organization": map[string]any{"data": map[string]string{"guid": target.orgGUID}}},
			})
		}
	}
	m.writeList(response, resources)
}

func (m *mockCF) serveApps(response http.ResponseWriter, request *http.Request) {
	spaceGUID := request.URL.Query().Get("space_guids")
	for _, target := range mockTargets {
		if target.spaceGUID == spaceGUID {
			m.writeList(response, []map[string]any{{
				"guid": "app-" + target.spaceGUID, "name": target.appName, "state": "STARTED",
				"relationships": map[string]any{"space": map[string]any{"data": map[string]string{"guid": target.spaceGUID}}},
			}})
			return
		}
	}
	m.writeList(response, nil)
}

func (m *mockCF) waitUntilReleased(response http.ResponseWriter, request *http.Request) {
	m.startOnce.Do(func() { close(m.blockStarted) })
	select {
	case <-m.blockRelease:
		m.writeJSON(response, http.StatusOK, map[string]bool{"released": true})
	case <-request.Context().Done():
	}
}

func (m *mockCF) waitAtBarrier(response http.ResponseWriter, request *http.Request) {
	m.mu.Lock()
	m.barrierArrivals++
	if m.barrierArrivals == 2 {
		m.barrierOnce.Do(func() { close(m.barrierRelease) })
	}
	m.mu.Unlock()

	select {
	case <-m.barrierRelease:
		m.writeJSON(response, http.StatusOK, map[string]string{"workspace": request.URL.Query().Get("workspace")})
	case <-time.After(10 * time.Second):
		m.writeJSON(response, http.StatusGatewayTimeout, map[string]string{"error": "parallel workspace barrier timed out"})
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

func (m *mockCF) sawAppRequest(spaceGUID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, request := range m.requests {
		if request.method == http.MethodGet && request.path == "/v3/apps" && request.query.Get("space_guids") == spaceGUID {
			return true
		}
	}
	return false
}

func (m *mockCF) barrierArrivalCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.barrierArrivals
}
