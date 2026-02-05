package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/api"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Validate required config
	if cfg.APIKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}

	// Initialize state manager
	stateMgr := state.NewStateManager()

	// Initialize Kubernetes client
	k8sClient, err := k8s.NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Initialize API handler
	handler := api.NewHandler(k8sClient, stateMgr, cfg)

	// Setup router
	router := mux.NewRouter()

	// Health check endpoints (no auth required) - must be registered before auth middleware
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
	authRouter.HandleFunc("/pause", handler.PauseRuntime).Methods("POST")
	authRouter.HandleFunc("/resume", handler.ResumeRuntime).Methods("POST")
	authRouter.HandleFunc("/list", handler.ListRuntimes).Methods("GET")
	authRouter.HandleFunc("/runtime/{runtime_id}", handler.GetRuntime).Methods("GET")
	authRouter.HandleFunc("/sessions/{session_id}", handler.GetSession).Methods("GET")
	authRouter.HandleFunc("/sessions/batch", handler.GetSessionsBatch).Methods("GET")
	authRouter.HandleFunc("/registry_prefix", handler.GetRegistryPrefix).Methods("GET")
	authRouter.HandleFunc("/image_exists", handler.CheckImageExists).Methods("GET")

	// Start server with timeouts
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("Starting OpenHands Kubernetes Runtime API server on %s", addr)
	log.Printf("Namespace: %s", cfg.Namespace)
	log.Printf("Base Domain: %s", cfg.BaseDomain)
	log.Printf("Registry Prefix: %s", cfg.RegistryPrefix)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
