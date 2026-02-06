package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/api"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
)

// setupTestRouter creates a router similar to main() for testing
func setupTestRouter() *mux.Router {
	cfg := &config.Config{
		ServerPort:      "8080",
		APIKey:          "test-api-key",
		Namespace:       "test",
		BaseDomain:      "test.example.com",
		Worker1Port:     12000,
		Worker2Port:     12001,
		AgentServerPort: 60000,
		DefaultImage:    "test-image",
	}
	stateMgr := state.NewStateManager()
	handler := api.NewHandler(nil, stateMgr, cfg)

	// Setup router same way as in main()
	router := mux.NewRouter()

	// Health check endpoints (no auth required)
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}
	router.HandleFunc("/health", healthHandler).Methods("GET")
	router.HandleFunc("/liveness", healthHandler).Methods("GET")
	router.HandleFunc("/readiness", healthHandler).Methods("GET")

	// Create a subrouter for authenticated routes
	authRouter := router.PathPrefix("/").Subrouter()
	authRouter.Use(handler.LoggingMiddleware)
	authRouter.Use(handler.AuthMiddleware)

	// Register authenticated routes
	authRouter.HandleFunc("/start", handler.StartRuntime).Methods("POST")
	authRouter.HandleFunc("/stop", handler.StopRuntime).Methods("POST")
	authRouter.HandleFunc("/list", handler.ListRuntimes).Methods("GET")
	authRouter.HandleFunc("/registry_prefix", handler.GetRegistryPrefix).Methods("GET")
	authRouter.HandleFunc("/image_exists", handler.CheckImageExists).Methods("GET")

	return router
}

func TestHealthEndpointsNoAuth(t *testing.T) {
	router := setupTestRouter()

	tests := []struct {
		name     string
		endpoint string
	}{
		{"Health endpoint", "/health"},
		{"Liveness endpoint", "/liveness"},
		{"Readiness endpoint", "/readiness"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" without authentication", func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.endpoint, nil)
			// Deliberately NOT setting X-API-Key header
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status 200 for %s without auth, got %d", tt.endpoint, rr.Code)
			}

			if rr.Body.String() != "OK" {
				t.Errorf("Expected body 'OK' for %s, got '%s'", tt.endpoint, rr.Body.String())
			}
		})
	}
}

func TestAuthenticatedEndpointsRequireAuth(t *testing.T) {
	router := setupTestRouter()

	tests := []struct {
		name     string
		method   string
		endpoint string
	}{
		{"List endpoint", "GET", "/list"},
		{"Registry prefix endpoint", "GET", "/registry_prefix"},
		{"Image exists endpoint", "GET", "/image_exists?image=test"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" requires authentication", func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.endpoint, nil)
			// Deliberately NOT setting X-API-Key header
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401 for %s without auth, got %d", tt.endpoint, rr.Code)
			}
		})

		t.Run(tt.name+" works with authentication", func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.endpoint, nil)
			req.Header.Set("X-API-Key", "test-api-key")
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// Should not be unauthorized (may still be other errors like 400/404/500, but not 401)
			if rr.Code == http.StatusUnauthorized {
				t.Errorf("Expected authenticated request to %s to not return 401, got %d", tt.endpoint, rr.Code)
			}
		})
	}
}
