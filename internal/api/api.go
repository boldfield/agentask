package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/boldfield/agentask/internal/store"
)

// Server wraps the HTTP server with its dependencies: store, auth token, and lease TTL.
type Server struct {
	mux       *http.ServeMux
	store     store.Store
	authToken string
	leaseTTL  time.Duration
}

// New creates a new API server with the given store, auth token, and lease TTL.
func New(s store.Store, authToken string, leaseTTL time.Duration) *Server {
	mux := http.NewServeMux()
	server := &Server{
		mux:       mux,
		store:     s,
		authToken: authToken,
		leaseTTL:  leaseTTL,
	}

	// Register handlers
	// GET /healthz is exempted from auth
	mux.HandleFunc("GET /healthz", server.handleHealthz)

	// Project endpoints (protected)
	mux.HandleFunc("POST /projects", server.authMiddleware(server.handleCreateProject))
	mux.HandleFunc("GET /projects/{id}", server.authMiddleware(server.handleGetProject))

	// Document endpoints (protected)
	mux.HandleFunc("POST /projects/{id}/documents", server.authMiddleware(server.handleCreateDocument))
	mux.HandleFunc("GET /projects/{id}/documents", server.authMiddleware(server.handleListDocuments))

	// Task endpoints (protected)
	mux.HandleFunc("POST /projects/{id}/tasks", server.authMiddleware(server.handleCreateTasks))
	mux.HandleFunc("GET /projects/{id}/tasks", server.authMiddleware(server.handleListTasks))
	mux.HandleFunc("GET /tasks/{id}", server.authMiddleware(server.handleGetTask))
	mux.HandleFunc("POST /tasks/{id}/claim", server.authMiddleware(server.handleClaimTask))
	mux.HandleFunc("POST /tasks/{id}/promote", server.authMiddleware(server.handlePromoteTask))

	return server
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

// handleHealthz handles GET /healthz (no auth required).
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// authMiddleware checks the Authorization header and returns a handler that requires auth.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.errorResponse(w, http.StatusUnauthorized, "MISSING_AUTH", "Missing Authorization header")
			return
		}

		// Extract bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			s.errorResponse(w, http.StatusUnauthorized, "INVALID_AUTH_FORMAT", "Authorization header must be 'Bearer <token>'")
			return
		}

		token := parts[1]
		// Constant-time compare to avoid leaking the token via timing.
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
			s.errorResponse(w, http.StatusUnauthorized, "INVALID_TOKEN", "Invalid authentication token")
			return
		}

		next(w, r)
	}
}

// decodeJSON decodes a JSON body and handles errors with appropriate responses.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if r.Body == nil {
		return errors.New("empty body")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.errorResponse(w, http.StatusBadRequest, "READ_ERROR", "Failed to read request body")
		return err
	}

	if err := json.Unmarshal(body, v); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "JSON_DECODE_ERROR", "Invalid JSON in request body")
		return err
	}

	return nil
}

// encodeJSON encodes a value as JSON with the given status code.
func (s *Server) encodeJSON(w http.ResponseWriter, statusCode int, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(v)
}

// errorResponse writes a consistent error response.
func (s *Server) errorResponse(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// Mux returns the underlying http.ServeMux for testing or direct access.
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// handleCreateProject handles POST /projects to create a new project.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name string `json:"name"`
		Repo string `json:"repo"`
	}

	if err := s.decodeJSON(w, r, &payload); err != nil {
		return // decodeJSON already wrote error response
	}

	// Validate name is non-empty
	if payload.Name == "" {
		s.errorResponse(w, http.StatusBadRequest, "EMPTY_NAME", "Project name cannot be empty")
		return
	}

	// Create the project (repo may be empty string)
	project, err := s.store.CreateProject(r.Context(), payload.Name, payload.Repo)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "CREATE_ERROR", "Failed to create project")
		return
	}

	s.encodeJSON(w, http.StatusCreated, project)
}

// handleGetProject handles GET /projects/{id} to retrieve a project.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.errorResponse(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
		return
	}
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "GET_ERROR", "Failed to get project")
		return
	}

	s.encodeJSON(w, http.StatusOK, project)
}

// handleCreateDocument handles POST /projects/{id}/documents to register a document.
func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var payload struct {
		Kind   string  `json:"kind"`
		Title  string  `json:"title"`
		Ref    string  `json:"ref"`
		Commit *string `json:"commit"`
	}

	if err := s.decodeJSON(w, r, &payload); err != nil {
		return // decodeJSON already wrote error response
	}

	// Validate kind is one of the allowed values
	if payload.Kind != "design" && payload.Kind != "feature_spec" {
		s.errorResponse(w, http.StatusBadRequest, "INVALID_KIND", "kind must be 'design' or 'feature_spec'")
		return
	}

	// Create the document
	doc, err := s.store.CreateDocument(r.Context(), id, payload.Kind, payload.Title, payload.Ref, payload.Commit)
	if errors.Is(err, store.ErrNotFound) {
		s.errorResponse(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		s.errorResponse(w, http.StatusConflict, "CONFLICT", "A design document already exists for this project")
		return
	}
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "CREATE_ERROR", "Failed to create document")
		return
	}

	s.encodeJSON(w, http.StatusCreated, doc)
}

// handleListDocuments handles GET /projects/{id}/documents to list documents.
func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Parse optional kind query parameter
	var kind *string
	if kindQuery := r.URL.Query().Get("kind"); kindQuery != "" {
		kind = &kindQuery
	}

	// List documents
	docs, err := s.store.ListDocuments(r.Context(), id, kind)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "LIST_ERROR", "Failed to list documents")
		return
	}

	// Ensure we return an empty array, not null
	if docs == nil {
		docs = make([]store.Document, 0)
	}

	s.encodeJSON(w, http.StatusOK, docs)
}

// handleCreateTasks handles POST /projects/{id}/tasks to bulk-create tasks.
func (s *Server) handleCreateTasks(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	var payload []store.TaskInput
	if err := s.decodeJSON(w, r, &payload); err != nil {
		return
	}

	tasks, err := s.store.CreateTasks(r.Context(), projectID, payload)
	if err != nil {
		// Client-input errors map to 400 with their specific code; everything else is 500.
		var ve *store.ValidationError
		if errors.As(err, &ve) {
			s.errorResponse(w, http.StatusBadRequest, ve.Code, ve.Error())
			return
		}
		s.errorResponse(w, http.StatusInternalServerError, "CREATE_ERROR", "Failed to create tasks")
		return
	}

	s.encodeJSON(w, http.StatusCreated, tasks)
}

// handleGetTask handles GET /tasks/{id} to retrieve a task with dependencies and links.
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	task, err := s.store.GetTask(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.errorResponse(w, http.StatusNotFound, "NOT_FOUND", "Task not found")
		return
	}
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "GET_ERROR", "Failed to get task")
		return
	}

	s.encodeJSON(w, http.StatusOK, task)
}

// handleListTasks handles GET /projects/{id}/tasks with optional filters.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	filter := store.TaskListFilter{}

	if state := r.URL.Query().Get("state"); state != "" {
		filter.State = &state
	}

	if assignee := r.URL.Query().Get("assignee"); assignee != "" {
		filter.Assignee = &assignee
	}

	if claimable := r.URL.Query().Get("claimable"); claimable == "true" {
		filter.Claimable = true
	}

	tasks, err := s.store.ListTasks(r.Context(), projectID, filter)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "LIST_ERROR", "Failed to list tasks")
		return
	}

	// Ensure we return an empty array, not null
	if tasks == nil {
		tasks = make([]store.Task, 0)
	}

	s.encodeJSON(w, http.StatusOK, tasks)
}

// handleClaimTask handles POST /tasks/{id}/claim to claim a task as in_progress.
func (s *Server) handleClaimTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")

	var payload struct {
		AgentID string `json:"agent_id"`
	}

	if err := s.decodeJSON(w, r, &payload); err != nil {
		return // decodeJSON already wrote error response
	}

	// Validate agent_id is non-empty
	if payload.AgentID == "" {
		s.errorResponse(w, http.StatusBadRequest, "EMPTY_AGENT_ID", "agent_id cannot be empty")
		return
	}

	// Claim the task
	task, err := s.store.ClaimTask(r.Context(), taskID, payload.AgentID, s.leaseTTL)
	if errors.Is(err, store.ErrNotFound) {
		s.errorResponse(w, http.StatusNotFound, "NOT_FOUND", "Task not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		s.errorResponse(w, http.StatusConflict, "CONFLICT", "Task is not claimable")
		return
	}
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "CLAIM_ERROR", "Failed to claim task")
		return
	}

	s.encodeJSON(w, http.StatusOK, task)
}

// handlePromoteTask handles POST /tasks/{id}/promote to promote a task from backlog to ready.
func (s *Server) handlePromoteTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")

	// Promote the task
	task, err := s.store.PromoteTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) {
		s.errorResponse(w, http.StatusNotFound, "NOT_FOUND", "Task not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		s.errorResponse(w, http.StatusConflict, "CONFLICT", "Task is not in backlog")
		return
	}
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "PROMOTE_ERROR", "Failed to promote task")
		return
	}

	s.encodeJSON(w, http.StatusOK, task)
}
