package main

import (
	"fmt"
	"log"
	"net/http"

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
	
	// Apply middlewares
	router.Use(handler.LoggingMiddleware)
	router.Use(handler.AuthMiddleware)

	// Register routes
	router.HandleFunc("/start", handler.StartRuntime).Methods("POST")
	router.HandleFunc("/stop", handler.StopRuntime).Methods("POST")
	router.HandleFunc("/pause", handler.PauseRuntime).Methods("POST")
	router.HandleFunc("/resume", handler.ResumeRuntime).Methods("POST")
	router.HandleFunc("/list", handler.ListRuntimes).Methods("GET")
	router.HandleFunc("/runtime/{runtime_id}", handler.GetRuntime).Methods("GET")
	router.HandleFunc("/sessions/{session_id}", handler.GetSession).Methods("GET")
	router.HandleFunc("/sessions/batch", handler.GetSessionsBatch).Methods("GET")
	router.HandleFunc("/registry_prefix", handler.GetRegistryPrefix).Methods("GET")
	router.HandleFunc("/image_exists", handler.CheckImageExists).Methods("GET")

	// Health check endpoint (no auth required)
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	// Start server
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("Starting OpenHands Kubernetes Runtime API server on %s", addr)
	log.Printf("Namespace: %s", cfg.Namespace)
	log.Printf("Base Domain: %s", cfg.BaseDomain)
	log.Printf("Registry Prefix: %s", cfg.RegistryPrefix)
	
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
