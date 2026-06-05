package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/boldfield/agentask/internal/store"
)

func setupTestServer(t *testing.T, authToken string) *Server {
	// Use in-memory database for testing
	s, err := store.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	return New(s, authToken)
}

// TestHealthzWithoutAuth verifies GET /healthz returns 200 without auth.
func TestHealthzWithoutAuth(t *testing.T) {
	server := setupTestServer(t, "test-token")

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
}

// TestProtectedRouteWithoutAuth verifies a protected route returns 401 without auth.
func TestProtectedRouteWithoutAuth(t *testing.T) {
	server := setupTestServer(t, "test-token")

	// Register a test protected handler
	server.mux.HandleFunc("GET /test-protected", server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))

	req := httptest.NewRequest("GET", "/test-protected", nil)
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	code, ok := errObj["code"].(string)
	if !ok || code == "" {
		t.Errorf("error response missing 'code' field")
	}

	message, ok := errObj["message"].(string)
	if !ok || message == "" {
		t.Errorf("error response missing 'message' field")
	}
}

// TestProtectedRouteWithWrongToken verifies a protected route returns 401 with wrong token.
func TestProtectedRouteWithWrongToken(t *testing.T) {
	server := setupTestServer(t, "correct-token")

	// Register a test protected handler
	server.mux.HandleFunc("GET /test-protected", server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))

	req := httptest.NewRequest("GET", "/test-protected", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "INVALID_TOKEN" {
		t.Errorf("expected error code 'INVALID_TOKEN', got %q", code)
	}
}

// TestProtectedRouteWithCorrectToken verifies a protected route proceeds with correct token.
func TestProtectedRouteWithCorrectToken(t *testing.T) {
	server := setupTestServer(t, "correct-token")

	// Register a test protected handler
	server.mux.HandleFunc("GET /test-protected", server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))

	req := httptest.NewRequest("GET", "/test-protected", nil)
	req.Header.Set("Authorization", "Bearer correct-token")
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["result"] != "ok" {
		t.Errorf("expected result 'ok', got %q", resp["result"])
	}
}

// TestMalformedJSONResponse verifies that a malformed JSON body returns 400 with error envelope.
func TestMalformedJSONResponse(t *testing.T) {
	server := setupTestServer(t, "test-token")

	// Register a test protected handler that tries to decode JSON
	server.mux.HandleFunc("POST /test-json", server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := server.decodeJSON(w, r, &payload); err != nil {
			return // decodeJSON already wrote error response
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))

	// Send malformed JSON
	body := []byte(`{"invalid json}`)
	req := httptest.NewRequest("POST", "/test-json", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "JSON_DECODE_ERROR" {
		t.Errorf("expected error code 'JSON_DECODE_ERROR', got %q", code)
	}
}

// TestCreateAndGetProjectRoundTrip verifies that creating and retrieving a project round-trips all fields.
func TestCreateAndGetProjectRoundTrip(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Create a project
	createPayload := map[string]string{
		"name": "test-project",
		"repo": "https://github.com/example/test-repo",
	}
	createBody, _ := json.Marshal(createPayload)
	createReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", createW.Code)
	}

	var createResp store.Project
	if err := json.NewDecoder(createW.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	// Verify created project fields
	if createResp.ID == "" {
		t.Error("created project missing id")
	}
	if createResp.Name != "test-project" {
		t.Errorf("expected name 'test-project', got %q", createResp.Name)
	}
	if createResp.Repo != "https://github.com/example/test-repo" {
		t.Errorf("expected repo 'https://github.com/example/test-repo', got %q", createResp.Repo)
	}
	if createResp.CreatedAt == "" {
		t.Error("created project missing created_at")
	}

	// Get the project
	getReq := httptest.NewRequest("GET", "/projects/"+createResp.ID, nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", getW.Code)
	}

	var getResp store.Project
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}

	// Verify retrieved project matches created project
	if getResp.ID != createResp.ID {
		t.Errorf("expected id %q, got %q", createResp.ID, getResp.ID)
	}
	if getResp.Name != createResp.Name {
		t.Errorf("expected name %q, got %q", createResp.Name, getResp.Name)
	}
	if getResp.Repo != createResp.Repo {
		t.Errorf("expected repo %q, got %q", createResp.Repo, getResp.Repo)
	}
	if getResp.CreatedAt != createResp.CreatedAt {
		t.Errorf("expected created_at %q, got %q", createResp.CreatedAt, getResp.CreatedAt)
	}
}

// TestGetProjectNotFound verifies that getting an unknown project returns 404.
func TestGetProjectNotFound(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	getReq := httptest.NewRequest("GET", "/projects/nonexistent-id", nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", getW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(getW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "NOT_FOUND" {
		t.Errorf("expected error code 'NOT_FOUND', got %q", code)
	}
}

// TestCreateProjectEmptyName verifies that creating a project with empty name returns 400.
func TestCreateProjectEmptyName(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	createPayload := map[string]string{
		"name": "",
		"repo": "https://github.com/example/test-repo",
	}
	createBody, _ := json.Marshal(createPayload)
	createReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", createW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(createW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "EMPTY_NAME" {
		t.Errorf("expected error code 'EMPTY_NAME', got %q", code)
	}
}
