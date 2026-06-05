package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/boldfield/agentask/internal/store"
)

// Server wraps the HTTP server with its dependencies: store and auth token.
type Server struct {
	mux       *http.ServeMux
	store     store.Store
	authToken string
}

// New creates a new API server with the given store and auth token.
func New(s store.Store, authToken string) *Server {
	mux := http.NewServeMux()
	server := &Server{
		mux:       mux,
		store:     s,
		authToken: authToken,
	}

	// Register handlers
	// GET /healthz is exempted from auth
	mux.HandleFunc("GET /healthz", server.handleHealthz)

	// Project endpoints (protected)
	mux.HandleFunc("POST /projects", server.authMiddleware(server.handleCreateProject))
	mux.HandleFunc("GET /projects/{id}", server.authMiddleware(server.handleGetProject))

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
