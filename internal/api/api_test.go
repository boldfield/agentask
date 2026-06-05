package api

import (
	"bytes"
	"context"
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
