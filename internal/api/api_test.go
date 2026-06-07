package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/store"
)

func setupTestServer(t *testing.T, authToken string) *Server {
	// Use in-memory database for testing
	s, err := store.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	return New(s, authToken, 5*time.Minute)
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

// TestCreateDocumentFeatureSpec verifies that registering a feature_spec and listing returns it.
func TestCreateDocumentFeatureSpec(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Create a project first
	projectPayload := map[string]string{
		"name": "test-project",
		"repo": "https://github.com/example/test-repo",
	}
	projectBody, _ := json.Marshal(projectPayload)
	projectReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(projectBody))
	projectReq.Header.Set("Authorization", authHeader)
	projectReq.Header.Set("Content-Type", "application/json")
	projectW := httptest.NewRecorder()
	server.mux.ServeHTTP(projectW, projectReq)

	var project store.Project
	json.NewDecoder(projectW.Body).Decode(&project)

	// Create a feature_spec document
	docPayload := map[string]interface{}{
		"kind":   "feature_spec",
		"title":  "Test Feature",
		"ref":    "docs/features/test.md",
		"commit": "abc123",
	}
	docBody, _ := json.Marshal(docPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(docBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", createW.Code)
	}

	var createdDoc store.Document
	if err := json.NewDecoder(createW.Body).Decode(&createdDoc); err != nil {
		t.Fatalf("failed to decode created document: %v", err)
	}

	if createdDoc.ID == "" {
		t.Error("created document missing id")
	}
	if createdDoc.Kind != "feature_spec" {
		t.Errorf("expected kind 'feature_spec', got %q", createdDoc.Kind)
	}
	if createdDoc.Title != "Test Feature" {
		t.Errorf("expected title 'Test Feature', got %q", createdDoc.Title)
	}

	// List documents
	listReq := httptest.NewRequest("GET", "/projects/"+project.ID+"/documents", nil)
	listReq.Header.Set("Authorization", authHeader)
	listW := httptest.NewRecorder()
	server.mux.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", listW.Code)
	}

	var docs []store.Document
	if err := json.NewDecoder(listW.Body).Decode(&docs); err != nil {
		t.Fatalf("failed to decode documents list: %v", err)
	}

	if len(docs) != 1 {
		t.Errorf("expected 1 document, got %d", len(docs))
	}
	if docs[0].ID != createdDoc.ID {
		t.Errorf("expected document id %q, got %q", createdDoc.ID, docs[0].ID)
	}
}

// TestSecondDesignConflict verifies that a second design for the same project returns 409.
func TestSecondDesignConflict(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Create a project
	projectPayload := map[string]string{
		"name": "test-project",
		"repo": "https://github.com/example/test-repo",
	}
	projectBody, _ := json.Marshal(projectPayload)
	projectReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(projectBody))
	projectReq.Header.Set("Authorization", authHeader)
	projectReq.Header.Set("Content-Type", "application/json")
	projectW := httptest.NewRecorder()
	server.mux.ServeHTTP(projectW, projectReq)

	var project store.Project
	json.NewDecoder(projectW.Body).Decode(&project)

	// Create first design document
	firstDocPayload := map[string]interface{}{
		"kind":  "design",
		"title": "Design Doc 1",
		"ref":   "DESIGN.md",
	}
	firstDocBody, _ := json.Marshal(firstDocPayload)
	firstReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(firstDocBody))
	firstReq.Header.Set("Authorization", authHeader)
	firstReq.Header.Set("Content-Type", "application/json")
	firstW := httptest.NewRecorder()
	server.mux.ServeHTTP(firstW, firstReq)

	if firstW.Code != http.StatusCreated {
		t.Errorf("expected first design to succeed with 201, got %d", firstW.Code)
	}

	// Try to create second design document
	secondDocPayload := map[string]interface{}{
		"kind":  "design",
		"title": "Design Doc 2",
		"ref":   "DESIGN2.md",
	}
	secondDocBody, _ := json.Marshal(secondDocPayload)
	secondReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(secondDocBody))
	secondReq.Header.Set("Authorization", authHeader)
	secondReq.Header.Set("Content-Type", "application/json")
	secondW := httptest.NewRecorder()
	server.mux.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusConflict {
		t.Errorf("expected second design to return 409, got %d", secondW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(secondW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "CONFLICT" {
		t.Errorf("expected error code 'CONFLICT', got %q", code)
	}
}

// TestInvalidDocumentKind verifies that an invalid kind returns 400.
func TestInvalidDocumentKind(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Create a project
	projectPayload := map[string]string{
		"name": "test-project",
		"repo": "https://github.com/example/test-repo",
	}
	projectBody, _ := json.Marshal(projectPayload)
	projectReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(projectBody))
	projectReq.Header.Set("Authorization", authHeader)
	projectReq.Header.Set("Content-Type", "application/json")
	projectW := httptest.NewRecorder()
	server.mux.ServeHTTP(projectW, projectReq)

	var project store.Project
	json.NewDecoder(projectW.Body).Decode(&project)

	// Try to create document with invalid kind
	docPayload := map[string]interface{}{
		"kind":  "invalid_kind",
		"title": "Test",
		"ref":   "test.md",
	}
	docBody, _ := json.Marshal(docPayload)
	docReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(docBody))
	docReq.Header.Set("Authorization", authHeader)
	docReq.Header.Set("Content-Type", "application/json")
	docW := httptest.NewRecorder()
	server.mux.ServeHTTP(docW, docReq)

	if docW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", docW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(docW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "INVALID_KIND" {
		t.Errorf("expected error code 'INVALID_KIND', got %q", code)
	}
}

// TestListDocumentsWithKindFilter verifies that the kind filter works.
func TestListDocumentsWithKindFilter(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Create a project
	projectPayload := map[string]string{
		"name": "test-project",
		"repo": "https://github.com/example/test-repo",
	}
	projectBody, _ := json.Marshal(projectPayload)
	projectReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(projectBody))
	projectReq.Header.Set("Authorization", authHeader)
	projectReq.Header.Set("Content-Type", "application/json")
	projectW := httptest.NewRecorder()
	server.mux.ServeHTTP(projectW, projectReq)

	var project store.Project
	json.NewDecoder(projectW.Body).Decode(&project)

	// Create a design document
	designPayload := map[string]interface{}{
		"kind":  "design",
		"title": "Design",
		"ref":   "DESIGN.md",
	}
	designBody, _ := json.Marshal(designPayload)
	designReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(designBody))
	designReq.Header.Set("Authorization", authHeader)
	designReq.Header.Set("Content-Type", "application/json")
	designW := httptest.NewRecorder()
	server.mux.ServeHTTP(designW, designReq)

	// Create feature_spec documents
	for i := 1; i <= 2; i++ {
		featurePayload := map[string]interface{}{
			"kind":  "feature_spec",
			"title": "Feature " + string(rune(48+i)),
			"ref":   "docs/features/f" + string(rune(48+i)) + ".md",
		}
		featureBody, _ := json.Marshal(featurePayload)
		featureReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(featureBody))
		featureReq.Header.Set("Authorization", authHeader)
		featureReq.Header.Set("Content-Type", "application/json")
		featureW := httptest.NewRecorder()
		server.mux.ServeHTTP(featureW, featureReq)
	}

	// List all documents
	allReq := httptest.NewRequest("GET", "/projects/"+project.ID+"/documents", nil)
	allReq.Header.Set("Authorization", authHeader)
	allW := httptest.NewRecorder()
	server.mux.ServeHTTP(allW, allReq)

	var allDocs []store.Document
	json.NewDecoder(allW.Body).Decode(&allDocs)
	if len(allDocs) != 3 {
		t.Errorf("expected 3 total documents, got %d", len(allDocs))
	}

	// List only feature_spec documents
	filterReq := httptest.NewRequest("GET", "/projects/"+project.ID+"/documents?kind=feature_spec", nil)
	filterReq.Header.Set("Authorization", authHeader)
	filterW := httptest.NewRecorder()
	server.mux.ServeHTTP(filterW, filterReq)

	var filteredDocs []store.Document
	json.NewDecoder(filterW.Body).Decode(&filteredDocs)
	if len(filteredDocs) != 2 {
		t.Errorf("expected 2 feature_spec documents, got %d", len(filteredDocs))
	}
	for _, doc := range filteredDocs {
		if doc.Kind != "feature_spec" {
			t.Errorf("expected kind 'feature_spec', got %q", doc.Kind)
		}
	}

	// List only design documents
	designFilterReq := httptest.NewRequest("GET", "/projects/"+project.ID+"/documents?kind=design", nil)
	designFilterReq.Header.Set("Authorization", authHeader)
	designFilterW := httptest.NewRecorder()
	server.mux.ServeHTTP(designFilterW, designFilterReq)

	var designDocs []store.Document
	json.NewDecoder(designFilterW.Body).Decode(&designDocs)
	if len(designDocs) != 1 {
		t.Errorf("expected 1 design document, got %d", len(designDocs))
	}
}

// Helper to create project and document for task tests
func setupProjectAndDocument(t *testing.T, server *Server, authHeader string) (string, string) {
	// Create a project
	projectPayload := map[string]string{
		"name": "test-project",
		"repo": "https://github.com/example/test-repo",
	}
	projectBody, _ := json.Marshal(projectPayload)
	projectReq := httptest.NewRequest("POST", "/projects", bytes.NewReader(projectBody))
	projectReq.Header.Set("Authorization", authHeader)
	projectReq.Header.Set("Content-Type", "application/json")
	projectW := httptest.NewRecorder()
	server.mux.ServeHTTP(projectW, projectReq)

	var project store.Project
	json.NewDecoder(projectW.Body).Decode(&project)

	// Create a document
	docPayload := map[string]interface{}{
		"kind":  "design",
		"title": "Design",
		"ref":   "DESIGN.md",
	}
	docBody, _ := json.Marshal(docPayload)
	docReq := httptest.NewRequest("POST", "/projects/"+project.ID+"/documents", bytes.NewReader(docBody))
	docReq.Header.Set("Authorization", authHeader)
	docReq.Header.Set("Content-Type", "application/json")
	docW := httptest.NewRecorder()
	server.mux.ServeHTTP(docW, docReq)

	var doc store.Document
	json.NewDecoder(docW.Body).Decode(&doc)

	return project.ID, doc.ID
}

// TestBulkCreateTasksWithIntraBatchDependency verifies bulk create persists tasks and edges.
func TestBulkCreateTasksWithIntraBatchDependency(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Bulk create two tasks where B depends on A (via A's batch key)
	taskPayload := []store.TaskInput{
		{
			Key:        "task-a",
			Title:      "Task A",
			Spec:       "Spec A",
			DocumentID: docID,
			DependsOn:  []string{},
		},
		{
			Key:        "task-b",
			Title:      "Task B",
			Spec:       "Spec B",
			DocumentID: docID,
			DependsOn:  []string{"task-a"},
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	req := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var createdTasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&createdTasks); err != nil {
		t.Fatalf("failed to decode created tasks: %v", err)
	}

	if len(createdTasks) != 2 {
		t.Errorf("expected 2 created tasks, got %d", len(createdTasks))
	}

	if createdTasks[0].Title != "Task A" || createdTasks[1].Title != "Task B" {
		t.Error("task titles do not match")
	}

	taskAID := createdTasks[0].ID
	taskBID := createdTasks[1].ID

	// Get Task B and verify its dependency on Task A
	getReq := httptest.NewRequest("GET", "/tasks/"+taskBID, nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", getW.Code)
	}

	var taskB store.TaskWithDepsAndLinks
	if err := json.NewDecoder(getW.Body).Decode(&taskB); err != nil {
		t.Fatalf("failed to decode task B: %v", err)
	}

	if len(taskB.DependsOn) != 1 || taskB.DependsOn[0] != taskAID {
		t.Errorf("expected task B to depend on %q, got %v", taskAID, taskB.DependsOn)
	}

	// Verify both tasks are in backlog state
	if createdTasks[0].State != "backlog" || createdTasks[1].State != "backlog" {
		t.Error("tasks not in backlog state")
	}
}

// TestListTasksWithStateFilter verifies state filter works.
func TestListTasksWithStateFilter(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create two tasks
	taskPayload := []store.TaskInput{
		{
			Title:      "Task 1",
			Spec:       "Spec 1",
			DocumentID: docID,
		},
		{
			Title:      "Task 2",
			Spec:       "Spec 2",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	// List with state=backlog filter
	listReq := httptest.NewRequest("GET", "/projects/"+projectID+"/tasks?state=backlog", nil)
	listReq.Header.Set("Authorization", authHeader)
	listW := httptest.NewRecorder()
	server.mux.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", listW.Code)
	}

	var tasks []store.Task
	if err := json.NewDecoder(listW.Body).Decode(&tasks); err != nil {
		t.Fatalf("failed to decode tasks: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 backlog tasks, got %d", len(tasks))
	}

	for _, task := range tasks {
		if task.State != "backlog" {
			t.Errorf("expected task state 'backlog', got %q", task.State)
		}
	}
}

// TestClaimableFilterExcludesUnfinishedDeps verifies a task with unfinished dependencies is excluded.
func TestClaimableFilterExcludesUnfinishedDeps(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create two tasks where B depends on A
	taskPayload := []store.TaskInput{
		{
			Key:        "task-a",
			Title:      "Task A",
			Spec:       "Spec A",
			DocumentID: docID,
		},
		{
			Key:        "task-b",
			Title:      "Task B",
			Spec:       "Spec B",
			DocumentID: docID,
			DependsOn:  []string{"task-a"},
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskAID := createdTasks[0].ID
	taskBID := createdTasks[1].ID

	// Update both to state=ready (directly in store for test setup)
	conn := server.store.Conn()
	_, err := conn.ExecContext(context.Background(), "UPDATE task SET state = 'ready' WHERE id IN (?, ?)", taskAID, taskBID)
	if err != nil {
		t.Fatalf("failed to promote tasks: %v", err)
	}

	// Query with claimable=true — A should be claimable (no deps, ready), B should NOT be (A not done)
	listReq := httptest.NewRequest("GET", "/projects/"+projectID+"/tasks?claimable=true", nil)
	listReq.Header.Set("Authorization", authHeader)
	listW := httptest.NewRecorder()
	server.mux.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", listW.Code)
	}

	var claimableTasks []store.Task
	json.NewDecoder(listW.Body).Decode(&claimableTasks)

	// Only A should be claimable (no deps, in ready state)
	if len(claimableTasks) != 1 {
		t.Errorf("expected 1 claimable task (A), got %d", len(claimableTasks))
	}
	if claimableTasks[0].ID != taskAID {
		t.Errorf("expected task A to be claimable, got %q", claimableTasks[0].ID)
	}

	// Now mark A as done
	_, err = conn.ExecContext(context.Background(), "UPDATE task SET state = 'done' WHERE id = ?", taskAID)
	if err != nil {
		t.Fatalf("failed to mark task A as done: %v", err)
	}

	// Query again — B should now be claimable (A is done, B is ready)
	listReq2 := httptest.NewRequest("GET", "/projects/"+projectID+"/tasks?claimable=true", nil)
	listReq2.Header.Set("Authorization", authHeader)
	listW2 := httptest.NewRecorder()
	server.mux.ServeHTTP(listW2, listReq2)

	var claimableTasks2 []store.Task
	json.NewDecoder(listW2.Body).Decode(&claimableTasks2)

	if len(claimableTasks2) != 1 {
		t.Errorf("expected 1 claimable task (B with A done), got %d", len(claimableTasks2))
	}
	if claimableTasks2[0].ID != taskBID {
		t.Errorf("expected task B to be claimable, got %q", claimableTasks2[0].ID)
	}
}

// TestCreateTasksUnknownDependency returns 400.
func TestCreateTasksUnknownDependency(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Try to create task with unknown dependency
	taskPayload := []store.TaskInput{
		{
			Title:      "Task A",
			Spec:       "Spec A",
			DocumentID: docID,
			DependsOn:  []string{"nonexistent-id"},
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	req := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	req.Header.Set("Authorization", authHeader)
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

	code, ok := errObj["code"].(string)
	if !ok || code != "UNKNOWN_DEPENDENCY" {
		t.Errorf("expected error code 'UNKNOWN_DEPENDENCY', got %q", code)
	}
}

// TestCreateTasksMissingDocumentID returns 400.
func TestCreateTasksMissingDocumentID(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, _ := setupProjectAndDocument(t, server, authHeader)

	// Try to create task without document_id
	taskPayload := []store.TaskInput{
		{
			Title: "Task A",
			Spec:  "Spec A",
			// Missing DocumentID
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	req := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestPromoteTaskBacklogToReady verifies promoting a backlog task to ready succeeds.
func TestPromoteTaskBacklogToReady(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a backlog task
	taskPayload := []store.TaskInput{
		{
			Title:      "Task to Promote",
			Spec:       "Promote this task",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Verify task is in backlog state
	if createdTasks[0].State != "backlog" {
		t.Errorf("expected initial state 'backlog', got %q", createdTasks[0].State)
	}

	// Promote the task
	promoteReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	if promoteW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", promoteW.Code)
	}

	var promotedTask store.Task
	if err := json.NewDecoder(promoteW.Body).Decode(&promotedTask); err != nil {
		t.Fatalf("failed to decode promoted task: %v", err)
	}

	// Verify state changed to ready
	if promotedTask.State != "ready" {
		t.Errorf("expected state 'ready', got %q", promotedTask.State)
	}
	if promotedTask.ID != taskID {
		t.Errorf("expected task id %q, got %q", taskID, promotedTask.ID)
	}
}

// TestPromoteTaskNotInBacklog verifies promoting a non-backlog task returns 409.
func TestPromoteTaskNotInBacklog(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a backlog task
	taskPayload := []store.TaskInput{
		{
			Title:      "Task",
			Spec:       "Spec",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// First promotion: backlog -> ready
	promoteReq1 := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq1.Header.Set("Authorization", authHeader)
	promoteW1 := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW1, promoteReq1)

	if promoteW1.Code != http.StatusOK {
		t.Errorf("expected first promotion to succeed with 200, got %d", promoteW1.Code)
	}

	// Second promotion: already in ready, should fail
	promoteReq2 := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq2.Header.Set("Authorization", authHeader)
	promoteW2 := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW2, promoteReq2)

	if promoteW2.Code != http.StatusConflict {
		t.Errorf("expected second promotion to return 409, got %d", promoteW2.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(promoteW2.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "CONFLICT" {
		t.Errorf("expected error code 'CONFLICT', got %q", code)
	}
}

// TestPromoteTaskUnknownID verifies promoting an unknown task returns 404.
func TestPromoteTaskUnknownID(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	promoteReq := httptest.NewRequest("POST", "/tasks/nonexistent-id/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	if promoteW.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", promoteW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(promoteW.Body).Decode(&errResp); err != nil {
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

// TestPromoteTaskAppendsTransitionEvent verifies that promoting a task creates a transition event.
func TestPromoteTaskAppendsTransitionEvent(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a backlog task
	taskPayload := []store.TaskInput{
		{
			Title:      "Task for Event",
			Spec:       "Check events",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Promote the task
	promoteReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	if promoteW.Code != http.StatusOK {
		t.Errorf("expected promotion to succeed with 200, got %d", promoteW.Code)
	}

	// Get the task with events
	getReq := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	var taskWithDeps store.TaskWithDepsAndLinks
	if err := json.NewDecoder(getW.Body).Decode(&taskWithDeps); err != nil {
		t.Fatalf("failed to decode task: %v", err)
	}

	// Fetch events directly from the store
	events, err := server.store.ListEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}

	if len(events) == 0 {
		t.Errorf("expected at least 1 event (transition), got %d", len(events))
	}

	// Verify the transition event
	transitionFound := false
	for _, event := range events {
		if event.Kind == "transition" && event.Actor == "system" {
			transitionFound = true
			if event.Note == nil || *event.Note != "backlog->ready" {
				t.Errorf("expected transition note 'backlog->ready', got %v", event.Note)
			}
		}
	}

	if !transitionFound {
		t.Error("transition event not found")
	}
}

// TestPromoteTaskRequiresAuth verifies promote endpoint requires auth.
func TestPromoteTaskRequiresAuth(t *testing.T) {
	server := setupTestServer(t, "test-token")

	promoteReq := httptest.NewRequest("POST", "/tasks/some-id/promote", nil)
	// No Authorization header
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	if promoteW.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", promoteW.Code)
	}
}

// Helper to set up a project, document, and an in_progress task with a lease
func setupClaimedTask(t *testing.T, server *Server, authHeader string) (string, *string) {
	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a task
	taskPayload := []store.TaskInput{
		{
			Title:      "Task to Claim",
			Spec:       "Test task for claiming",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Promote the task to ready
	promoteReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	// Claim the task
	claimPayload := map[string]string{"agent_id": "test-agent"}
	claimBody, _ := json.Marshal(claimPayload)
	claimReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Authorization", authHeader)
	claimReq.Header.Set("Content-Type", "application/json")
	claimW := httptest.NewRecorder()
	server.mux.ServeHTTP(claimW, claimReq)

	if claimW.Code != http.StatusOK {
		t.Fatalf("failed to claim task: got status %d", claimW.Code)
	}

	var claimedTask store.Task
	json.NewDecoder(claimW.Body).Decode(&claimedTask)

	return taskID, claimedTask.LeaseExpiresAt
}

// TestHeartbeatExtendsLease verifies that heartbeat by the assignee extends the lease.
func TestHeartbeatExtendsLease(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID, oldLeaseExpiry := setupClaimedTask(t, server, authHeader)

	// Small sleep to ensure time passes and new timestamp will be > old
	time.Sleep(10 * time.Millisecond)

	// Heartbeat the task with the same agent
	heartbeatPayload := map[string]string{"agent_id": "test-agent"}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Authorization", authHeader)
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", heartbeatW.Code)
	}

	var heartbeatTask store.Task
	if err := json.NewDecoder(heartbeatW.Body).Decode(&heartbeatTask); err != nil {
		t.Fatalf("failed to decode heartbeat response: %v", err)
	}

	// Verify lease was extended
	if heartbeatTask.LeaseExpiresAt == nil {
		t.Fatal("lease_expires_at is nil after heartbeat")
	}
	if oldLeaseExpiry == nil {
		t.Fatal("old lease_expires_at is nil")
	}
	if *heartbeatTask.LeaseExpiresAt <= *oldLeaseExpiry {
		t.Errorf("new lease expiry (%q) not greater than old (%q)", *heartbeatTask.LeaseExpiresAt, *oldLeaseExpiry)
	}
}

// TestHeartbeatByDifferentAgentReturns409 verifies that heartbeat by a non-assignee returns 409.
func TestHeartbeatByDifferentAgentReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID, _ := setupClaimedTask(t, server, authHeader)

	// Heartbeat with a different agent ID
	heartbeatPayload := map[string]string{"agent_id": "different-agent"}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Authorization", authHeader)
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", heartbeatW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(heartbeatW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "CONFLICT" {
		t.Errorf("expected error code 'CONFLICT', got %q", code)
	}
}

// TestHeartbeatOnNotInProgressReturns409 verifies that heartbeat on non-in_progress task returns 409.
func TestHeartbeatOnNotInProgressReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a task but keep it in backlog (don't promote/claim)
	taskPayload := []store.TaskInput{
		{
			Title:      "Backlog Task",
			Spec:       "This stays in backlog",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Try to heartbeat a backlog task
	heartbeatPayload := map[string]string{"agent_id": "test-agent"}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Authorization", authHeader)
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", heartbeatW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(heartbeatW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "CONFLICT" {
		t.Errorf("expected error code 'CONFLICT', got %q", code)
	}
}

// TestHeartbeatUnknownTaskReturns404 verifies that heartbeat on unknown task returns 404.
func TestHeartbeatUnknownTaskReturns404(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	heartbeatPayload := map[string]string{"agent_id": "test-agent"}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/nonexistent-id/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Authorization", authHeader)
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", heartbeatW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(heartbeatW.Body).Decode(&errResp); err != nil {
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

// TestHeartbeatEmptyAgentIDReturns400 verifies that empty agent_id returns 400.
func TestHeartbeatEmptyAgentIDReturns400(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID, _ := setupClaimedTask(t, server, authHeader)

	// Heartbeat with empty agent_id
	heartbeatPayload := map[string]string{"agent_id": ""}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Authorization", authHeader)
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", heartbeatW.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(heartbeatW.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error response missing 'error' field")
	}

	if code, ok := errObj["code"].(string); !ok || code != "EMPTY_AGENT_ID" {
		t.Errorf("expected error code 'EMPTY_AGENT_ID', got %q", code)
	}
}

// TestHeartbeatAppendsHeartbeatEvent verifies that a heartbeat appends a heartbeat event.
func TestHeartbeatAppendsHeartbeatEvent(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID, _ := setupClaimedTask(t, server, authHeader)

	// Get events before heartbeat
	eventsBefore, err := server.store.ListEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("failed to list events before heartbeat: %v", err)
	}

	// Heartbeat the task
	heartbeatPayload := map[string]string{"agent_id": "test-agent"}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Authorization", authHeader)
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", heartbeatW.Code)
	}

	// Get events after heartbeat
	eventsAfter, err := server.store.ListEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("failed to list events after heartbeat: %v", err)
	}

	// Verify a new heartbeat event was appended
	if len(eventsAfter) <= len(eventsBefore) {
		t.Errorf("expected more events after heartbeat, got same count: %d", len(eventsAfter))
	}

	// Find the heartbeat event
	heartbeatFound := false
	for _, event := range eventsAfter {
		if event.Kind == "heartbeat" && event.Actor == "test-agent" {
			heartbeatFound = true
			break
		}
	}

	if !heartbeatFound {
		t.Error("heartbeat event not found in events after heartbeat")
	}
}

// TestHeartbeatRequiresAuth verifies that heartbeat endpoint requires auth.
func TestHeartbeatRequiresAuth(t *testing.T) {
	server := setupTestServer(t, "test-token")

	heartbeatPayload := map[string]string{"agent_id": "test-agent"}
	heartbeatBody, _ := json.Marshal(heartbeatPayload)
	heartbeatReq := httptest.NewRequest("POST", "/tasks/some-id/heartbeat", bytes.NewReader(heartbeatBody))
	// No Authorization header
	heartbeatW := httptest.NewRecorder()
	server.mux.ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", heartbeatW.Code)
	}
}

// TestSubmitTaskMovesToReviewAndPersistsLinks verifies that submit moves a task to review,
// stores the result, persists links, and clears the lease.
func TestSubmitTaskMovesToReviewAndPersistsLinks(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Setup: project, document, and claimed task
	taskID, _ := setupClaimedTask(t, server, authHeader)

	// Submit with a result and two links (pr and commit)
	submitPayload := map[string]interface{}{
		"agent_id": "test-agent",
		"result":   "Work completed successfully",
		"links": []map[string]string{
			{"kind": "pr", "value": "https://github.com/repo/pull/123"},
			{"kind": "commit", "value": "abc123def456"},
		},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", submitW.Code, submitW.Body.String())
	}

	var submittedTask store.TaskWithDepsAndLinks
	if err := json.NewDecoder(submitW.Body).Decode(&submittedTask); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify task state is review
	if submittedTask.State != "review" {
		t.Errorf("expected state 'review', got %q", submittedTask.State)
	}

	// Verify result is stored
	if submittedTask.Result == nil || *submittedTask.Result != "Work completed successfully" {
		t.Errorf("expected result 'Work completed successfully', got %v", submittedTask.Result)
	}

	// Verify lease is cleared (NULL)
	if submittedTask.LeaseExpiresAt != nil {
		t.Errorf("expected lease_expires_at to be NULL, got %v", submittedTask.LeaseExpiresAt)
	}

	// Verify links are persisted and returned
	if len(submittedTask.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(submittedTask.Links))
	}

	// Check the first link (pr)
	found := false
	for _, link := range submittedTask.Links {
		if link.Kind == "pr" && link.Value == "https://github.com/repo/pull/123" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected link (pr, https://github.com/repo/pull/123) not found in response")
	}

	// Check the second link (commit)
	found = false
	for _, link := range submittedTask.Links {
		if link.Kind == "commit" && link.Value == "abc123def456" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected link (commit, abc123def456) not found in response")
	}

	// Verify links are retrievable via GET /tasks/{id}
	getReq := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET /tasks/{id} failed with status %d", getW.Code)
	}

	var retrievedTask store.TaskWithDepsAndLinks
	if err := json.NewDecoder(getW.Body).Decode(&retrievedTask); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}

	if len(retrievedTask.Links) != 2 {
		t.Errorf("expected 2 links via GET, got %d", len(retrievedTask.Links))
	}

	// Verify links are queryable by (kind, value) via the store's Conn
	// For this test, we just verify they exist in the retrieved task
	linkFound := false
	for _, link := range retrievedTask.Links {
		if link.Kind == "pr" && link.Value == "https://github.com/repo/pull/123" {
			linkFound = true
			break
		}
	}
	if !linkFound {
		t.Errorf("link (pr, ...) not retrievable via GET")
	}
}

// TestSubmitByDifferentAgentReturns409 verifies that submit by a different agent returns 409.
func TestSubmitByDifferentAgentReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Setup: project, document, and claimed task
	taskID, _ := setupClaimedTask(t, server, authHeader)

	// Try to submit with a different agent_id
	submitPayload := map[string]interface{}{
		"agent_id": "different-agent",
		"result":   "Work completed",
		"links":    []map[string]string{},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", submitW.Code)
	}
}

// TestSubmitFromNonInProgressReturns409 verifies that submit from a non-in_progress task returns 409.
func TestSubmitFromNonInProgressReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"
	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a task (it starts in backlog)
	taskPayload := []store.TaskInput{
		{
			Title:      "Task in backlog",
			Spec:       "Test task",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Try to submit without claiming (task is still in backlog, not in_progress)
	submitPayload := map[string]interface{}{
		"agent_id": "test-agent",
		"result":   "Work completed",
		"links":    []map[string]string{},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", submitW.Code)
	}
}

// TestSubmitUnknownTaskReturns404 verifies that submit on an unknown task returns 404.
func TestSubmitUnknownTaskReturns404(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	submitPayload := map[string]interface{}{
		"agent_id": "test-agent",
		"result":   "Work completed",
		"links":    []map[string]string{},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/unknown-task-id/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", submitW.Code)
	}
}

// TestSubmitWithInvalidLinkKindReturns400 verifies that submit with an invalid link kind returns 400
// and nothing changes (task still in_progress, no links).
func TestSubmitWithInvalidLinkKindReturns400(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Setup: project, document, and claimed task
	taskID, _ := setupClaimedTask(t, server, authHeader)

	// Try to submit with an invalid link kind
	submitPayload := map[string]interface{}{
		"agent_id": "test-agent",
		"result":   "Work completed",
		"links": []map[string]string{
			{"kind": "invalid_kind", "value": "some-value"},
		},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", submitW.Code, submitW.Body.String())
	}

	// Verify the error code is INVALID_LINK_KIND
	var errResp map[string]interface{}
	json.NewDecoder(submitW.Body).Decode(&errResp)
	errObj := errResp["error"].(map[string]interface{})
	if errObj["code"] != "INVALID_LINK_KIND" {
		t.Errorf("expected error code 'INVALID_LINK_KIND', got %v", errObj["code"])
	}

	// Verify task is still in_progress and unchanged
	getReq := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	var task store.TaskWithDepsAndLinks
	json.NewDecoder(getW.Body).Decode(&task)

	if task.State != "in_progress" {
		t.Errorf("expected task to still be in_progress, got %q", task.State)
	}

	if len(task.Links) != 0 {
		t.Errorf("expected no links, but task has %d links", len(task.Links))
	}
}

// TestSubmitEmptyAgentIDReturns400 verifies that submit with empty agent_id returns 400.
func TestSubmitEmptyAgentIDReturns400(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a task
	taskPayload := []store.TaskInput{
		{
			Title:      "Task",
			Spec:       "Test task",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Promote to ready
	promoteReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	// Claim the task
	claimPayload := map[string]string{"agent_id": "test-agent"}
	claimBody, _ := json.Marshal(claimPayload)
	claimReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Authorization", authHeader)
	claimReq.Header.Set("Content-Type", "application/json")
	claimW := httptest.NewRecorder()
	server.mux.ServeHTTP(claimW, claimReq)

	// Try to submit with empty agent_id
	submitPayload := map[string]interface{}{
		"agent_id": "",
		"result":   "Work completed",
		"links":    []map[string]string{},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", submitW.Code)
	}

	var errResp map[string]interface{}
	json.NewDecoder(submitW.Body).Decode(&errResp)
	errObj := errResp["error"].(map[string]interface{})
	if errObj["code"] != "EMPTY_AGENT_ID" {
		t.Errorf("expected error code 'EMPTY_AGENT_ID', got %v", errObj["code"])
	}
}

// TestSubmitRequiresAuth verifies that submit endpoint requires auth.
func TestSubmitRequiresAuth(t *testing.T) {
	server := setupTestServer(t, "test-token")

	submitPayload := map[string]interface{}{
		"agent_id": "test-agent",
		"result":   "Work completed",
		"links":    []map[string]string{},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/some-id/submit", bytes.NewReader(submitBody))
	// No Authorization header
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", submitW.Code)
	}
}

// Helper to set up a task in review state (for review and transition tests).
func setupTaskInReview(t *testing.T, server *Server, authHeader string) string {
	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a task
	taskPayload := []store.TaskInput{
		{
			Title:      "Task for Review",
			Spec:       "Test task for review",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Promote the task to ready
	promoteReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	// Claim the task
	claimPayload := map[string]string{"agent_id": "test-agent"}
	claimBody, _ := json.Marshal(claimPayload)
	claimReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Authorization", authHeader)
	claimReq.Header.Set("Content-Type", "application/json")
	claimW := httptest.NewRecorder()
	server.mux.ServeHTTP(claimW, claimReq)

	// Submit the task to move it to review state
	submitPayload := map[string]interface{}{
		"agent_id": "test-agent",
		"result":   "Work completed",
		"links":    []map[string]string{},
	}
	submitBody, _ := json.Marshal(submitPayload)
	submitReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", authHeader)
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	server.mux.ServeHTTP(submitW, submitReq)

	if submitW.Code != http.StatusOK {
		t.Fatalf("failed to submit task to review state: got status %d", submitW.Code)
	}

	return taskID
}

// TestReviewWithApproveVerdictSucceeds verifies that posting an approve review succeeds.
func TestReviewWithApproveVerdictSucceeds(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Post an approve review
	reviewPayload := map[string]interface{}{
		"actor":   "reviewer-1",
		"verdict": "approve",
		"note":    "Looks good!",
	}
	reviewBody, _ := json.Marshal(reviewPayload)
	reviewReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/review", bytes.NewReader(reviewBody))
	reviewReq.Header.Set("Authorization", authHeader)
	reviewReq.Header.Set("Content-Type", "application/json")
	reviewW := httptest.NewRecorder()
	server.mux.ServeHTTP(reviewW, reviewReq)

	if reviewW.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", reviewW.Code, reviewW.Body.String())
	}

	var event store.Event
	if err := json.NewDecoder(reviewW.Body).Decode(&event); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify event details
	if event.TaskID != taskID {
		t.Errorf("expected task_id %q, got %q", taskID, event.TaskID)
	}
	if event.Actor != "reviewer-1" {
		t.Errorf("expected actor 'reviewer-1', got %q", event.Actor)
	}
	if event.Kind != "review" {
		t.Errorf("expected kind 'review', got %q", event.Kind)
	}
	if event.Verdict == nil || *event.Verdict != "approve" {
		t.Errorf("expected verdict 'approve', got %v", event.Verdict)
	}
	if event.Note == nil || *event.Note != "Looks good!" {
		t.Errorf("expected note 'Looks good!', got %v", event.Note)
	}
}

// TestTransitionToDoneAfterApproveSucceeds verifies that transition to done succeeds from approved state.
func TestTransitionToDoneAfterApproveSucceeds(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Manually set task state to approved (simulating MR-9 verdict aggregation)
	_, err := server.store.Conn().ExecContext(context.Background(),
		"UPDATE task SET state = ? WHERE id = ?", "approved", taskID)
	if err != nil {
		t.Fatalf("failed to set task to approved: %v", err)
	}

	// Now transition from approved to done
	transitionPayload := map[string]interface{}{
		"to":   "done",
		"note": "Approved and merged to main",
	}
	transitionBody, _ := json.Marshal(transitionPayload)
	transitionReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/transition", bytes.NewReader(transitionBody))
	transitionReq.Header.Set("Authorization", authHeader)
	transitionReq.Header.Set("Content-Type", "application/json")
	transitionW := httptest.NewRecorder()
	server.mux.ServeHTTP(transitionW, transitionReq)

	if transitionW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", transitionW.Code, transitionW.Body.String())
	}

	var task store.Task
	if err := json.NewDecoder(transitionW.Body).Decode(&task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if task.State != "done" {
		t.Errorf("expected state 'done', got %q", task.State)
	}
}

// TestTransitionFromReviewToDoneReturns409 verifies that transition to done from review state is no longer allowed.
func TestTransitionFromReviewToDoneReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Try to transition directly from review to done (should fail)
	transitionPayload := map[string]interface{}{
		"to": "done",
	}
	transitionBody, _ := json.Marshal(transitionPayload)
	transitionReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/transition", bytes.NewReader(transitionBody))
	transitionReq.Header.Set("Authorization", authHeader)
	transitionReq.Header.Set("Content-Type", "application/json")
	transitionW := httptest.NewRecorder()
	server.mux.ServeHTTP(transitionW, transitionReq)

	if transitionW.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", transitionW.Code)
	}

	// Verify task state is still review
	getReq := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	getReq.Header.Set("Authorization", authHeader)
	getW := httptest.NewRecorder()
	server.mux.ServeHTTP(getW, getReq)

	var task store.TaskWithDepsAndLinks
	json.NewDecoder(getW.Body).Decode(&task)

	if task.State != "review" {
		t.Errorf("expected task to still be in 'review', got %q", task.State)
	}
}

func TestTransitionFromReviewToReadyReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Try to transition from review to ready (should fail, only allowed from approved)
	transitionPayload := map[string]interface{}{
		"to": "ready",
	}
	transitionBody, _ := json.Marshal(transitionPayload)
	transitionReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/transition", bytes.NewReader(transitionBody))
	transitionReq.Header.Set("Authorization", authHeader)
	transitionReq.Header.Set("Content-Type", "application/json")
	transitionW := httptest.NewRecorder()
	server.mux.ServeHTTP(transitionW, transitionReq)

	if transitionW.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", transitionW.Code)
	}
}

// TestTransitionToReadyFromApprovedReturnsToClaimablePool verifies that transition to ready from approved allows reclaiming.
func TestTransitionToReadyFromApprovedReturnsToClaimablePool(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Manually set task state to approved (simulating MR-9 verdict aggregation)
	_, err := server.store.Conn().ExecContext(context.Background(),
		"UPDATE task SET state = ? WHERE id = ?", "approved", taskID)
	if err != nil {
		t.Fatalf("failed to set task to approved: %v", err)
	}

	// Transition back to ready from approved (human overrides approval for rework)
	transitionPayload := map[string]interface{}{
		"to":   "ready",
		"note": "Needs rework",
	}
	transitionBody, _ := json.Marshal(transitionPayload)
	transitionReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/transition", bytes.NewReader(transitionBody))
	transitionReq.Header.Set("Authorization", authHeader)
	transitionReq.Header.Set("Content-Type", "application/json")
	transitionW := httptest.NewRecorder()
	server.mux.ServeHTTP(transitionW, transitionReq)

	if transitionW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", transitionW.Code)
	}

	var task store.Task
	if err := json.NewDecoder(transitionW.Body).Decode(&task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if task.State != "ready" {
		t.Errorf("expected state 'ready', got %q", task.State)
	}

	// Verify it can be claimed again
	claimPayload := map[string]string{"agent_id": "test-agent-2"}
	claimBody, _ := json.Marshal(claimPayload)
	claimReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Authorization", authHeader)
	claimReq.Header.Set("Content-Type", "application/json")
	claimW := httptest.NewRecorder()
	server.mux.ServeHTTP(claimW, claimReq)

	if claimW.Code != http.StatusOK {
		t.Errorf("expected claim to succeed (status 200), got %d", claimW.Code)
	}
}

// TestReviewOnNonReviewTaskReturns409 verifies that posting a review on a non-review task fails.
func TestReviewOnNonReviewTaskReturns409(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	// Create a task but leave it in backlog (no promote, no claim)
	taskPayload := []store.TaskInput{
		{
			Title:      "Backlog Task",
			Spec:       "This stays in backlog",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Try to post a review on a backlog task
	reviewPayload := map[string]interface{}{
		"actor":   "reviewer",
		"verdict": "approve",
	}
	reviewBody, _ := json.Marshal(reviewPayload)
	reviewReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/review", bytes.NewReader(reviewBody))
	reviewReq.Header.Set("Authorization", authHeader)
	reviewReq.Header.Set("Content-Type", "application/json")
	reviewW := httptest.NewRecorder()
	server.mux.ServeHTTP(reviewW, reviewReq)

	if reviewW.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", reviewW.Code)
	}
}

// TestReviewWithInvalidVerdictReturns400 verifies that posting a review with invalid verdict returns 400.
func TestReviewWithInvalidVerdictReturns400(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Post a review with invalid verdict
	reviewPayload := map[string]interface{}{
		"actor":   "reviewer",
		"verdict": "invalid-verdict",
	}
	reviewBody, _ := json.Marshal(reviewPayload)
	reviewReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/review", bytes.NewReader(reviewBody))
	reviewReq.Header.Set("Authorization", authHeader)
	reviewReq.Header.Set("Content-Type", "application/json")
	reviewW := httptest.NewRecorder()
	server.mux.ServeHTTP(reviewW, reviewReq)

	if reviewW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", reviewW.Code)
	}

	var errResp map[string]interface{}
	json.NewDecoder(reviewW.Body).Decode(&errResp)
	errObj := errResp["error"].(map[string]interface{})
	if errObj["code"] != "INVALID_VERDICT" {
		t.Errorf("expected error code 'INVALID_VERDICT', got %v", errObj["code"])
	}
}

// TestTransitionWithInvalidTargetStateReturns400 verifies that transition with invalid target state returns 400.
func TestTransitionWithInvalidTargetStateReturns400(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	taskID := setupTaskInReview(t, server, authHeader)

	// Try to transition to an invalid state
	transitionPayload := map[string]interface{}{
		"to": "invalid-state",
	}
	transitionBody, _ := json.Marshal(transitionPayload)
	transitionReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/transition", bytes.NewReader(transitionBody))
	transitionReq.Header.Set("Authorization", authHeader)
	transitionReq.Header.Set("Content-Type", "application/json")
	transitionW := httptest.NewRecorder()
	server.mux.ServeHTTP(transitionW, transitionReq)

	if transitionW.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", transitionW.Code)
	}

	var errResp map[string]interface{}
	json.NewDecoder(transitionW.Body).Decode(&errResp)
	errObj := errResp["error"].(map[string]interface{})
	if errObj["code"] != "INVALID_TARGET_STATE" {
		t.Errorf("expected error code 'INVALID_TARGET_STATE', got %v", errObj["code"])
	}
}

// TestTransitionToBlockedFromActiveStateSucceeds verifies that transition to blocked from an active state succeeds.
func TestTransitionToBlockedFromActiveStateSucceeds(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Set up a task in in_progress state
	projectID, docID := setupProjectAndDocument(t, server, authHeader)

	taskPayload := []store.TaskInput{
		{
			Title:      "Task to Block",
			Spec:       "Test task",
			DocumentID: docID,
		},
	}
	taskBody, _ := json.Marshal(taskPayload)
	createReq := httptest.NewRequest("POST", "/projects/"+projectID+"/tasks", bytes.NewReader(taskBody))
	createReq.Header.Set("Authorization", authHeader)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	server.mux.ServeHTTP(createW, createReq)

	var createdTasks []store.Task
	json.NewDecoder(createW.Body).Decode(&createdTasks)
	taskID := createdTasks[0].ID

	// Promote and claim
	promoteReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/promote", nil)
	promoteReq.Header.Set("Authorization", authHeader)
	promoteW := httptest.NewRecorder()
	server.mux.ServeHTTP(promoteW, promoteReq)

	claimPayload := map[string]string{"agent_id": "test-agent"}
	claimBody, _ := json.Marshal(claimPayload)
	claimReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Authorization", authHeader)
	claimReq.Header.Set("Content-Type", "application/json")
	claimW := httptest.NewRecorder()
	server.mux.ServeHTTP(claimW, claimReq)

	// Transition to blocked
	transitionPayload := map[string]interface{}{
		"to":   "blocked",
		"note": "Blocked on external dependency",
	}
	transitionBody, _ := json.Marshal(transitionPayload)
	transitionReq := httptest.NewRequest("POST", "/tasks/"+taskID+"/transition", bytes.NewReader(transitionBody))
	transitionReq.Header.Set("Authorization", authHeader)
	transitionReq.Header.Set("Content-Type", "application/json")
	transitionW := httptest.NewRecorder()
	server.mux.ServeHTTP(transitionW, transitionReq)

	if transitionW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", transitionW.Code)
	}

	var task store.Task
	if err := json.NewDecoder(transitionW.Body).Decode(&task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if task.State != "blocked" {
		t.Errorf("expected state 'blocked', got %q", task.State)
	}
}

// TestListProjectsReturnsAllProjects verifies that GET /projects returns all created projects ordered by created_at.
func TestListProjectsReturnsAllProjects(t *testing.T) {
	server := setupTestServer(t, "test-token")
	authHeader := "Bearer test-token"

	// Create multiple projects
	projects := []map[string]string{
		{"name": "list-test-project-a", "repo": "https://github.com/example/repo-a"},
		{"name": "list-test-project-b", "repo": "https://github.com/example/repo-b"},
		{"name": "list-test-project-c", "repo": "https://github.com/example/repo-c"},
	}

	createdProjects := make([]store.Project, len(projects))
	for i, p := range projects {
		payload := map[string]string{"name": p["name"], "repo": p["repo"]}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/projects", bytes.NewReader(body))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.mux.ServeHTTP(w, req)

		var resp store.Project
		json.NewDecoder(w.Body).Decode(&resp)
		createdProjects[i] = resp
	}

	// List all projects
	listReq := httptest.NewRequest("GET", "/projects", nil)
	listReq.Header.Set("Authorization", authHeader)
	listW := httptest.NewRecorder()
	server.mux.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", listW.Code)
	}

	var respProjects []store.Project
	if err := json.NewDecoder(listW.Body).Decode(&respProjects); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify all created projects are in the response
	for _, created := range createdProjects {
		found := false
		for _, resp := range respProjects {
			if resp.ID == created.ID {
				// Verify the fields match
				if resp.Name != created.Name {
					t.Errorf("expected name %q, got %q", created.Name, resp.Name)
				}
				if resp.Repo != created.Repo {
					t.Errorf("expected repo %q, got %q", created.Repo, resp.Repo)
				}
				if resp.CreatedAt != created.CreatedAt {
					t.Errorf("expected created_at %q, got %q", created.CreatedAt, resp.CreatedAt)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created project %q not found in list response", created.ID)
		}
	}

	// Verify projects are ordered by created_at
	for i := 1; i < len(respProjects); i++ {
		if respProjects[i].CreatedAt < respProjects[i-1].CreatedAt {
			t.Errorf("projects not ordered by created_at: %s >= %s", respProjects[i-1].CreatedAt, respProjects[i].CreatedAt)
		}
	}
}

// TestListProjectsWithoutAuth verifies that GET /projects returns 401 without a token.
func TestListProjectsWithoutAuth(t *testing.T) {
	server := setupTestServer(t, "test-token")

	listReq := httptest.NewRequest("GET", "/projects", nil)
	listW := httptest.NewRecorder()
	server.mux.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", listW.Code)
	}
}

// TestListProjectsReturnsEmptyArray verifies that GET /projects returns [] (not null) when no projects exist.
// This test uses a fresh database to ensure no prior projects exist.
func TestListProjectsReturnsEmptyArray(t *testing.T) {
	tmpdb := t.TempDir() + "/test.db"
	s, err := store.Open(tmpdb)
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	server := New(s, "test-token", 5*time.Minute)
	authHeader := "Bearer test-token"

	// List projects without creating any
	listReq := httptest.NewRequest("GET", "/projects", nil)
	listReq.Header.Set("Authorization", authHeader)
	listW := httptest.NewRecorder()
	server.mux.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", listW.Code)
	}

	var respProjects []store.Project
	if err := json.NewDecoder(listW.Body).Decode(&respProjects); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if respProjects == nil {
		t.Error("expected empty array, got nil")
	}
	if len(respProjects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(respProjects))
	}
}
