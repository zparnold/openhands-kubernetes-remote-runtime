package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/api"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/reaper"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Initialize logger with configured level
	logger.Init(cfg.LogLevel)
	logger.Info("Initializing OpenHands Kubernetes Runtime API")
	logger.Debug("Log level set to: %s", cfg.LogLevel)

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

	// Initialize and start idle sandbox reaper
	reaperInstance := reaper.NewReaper(stateMgr, k8sClient, cfg)
	reaperInstance.Start()

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
	authRouter.HandleFunc("/sessions/batch", handler.GetSessionsBatch).Methods("GET")
	authRouter.HandleFunc("/sessions/{session_id}", handler.GetSession).Methods("GET")
	authRouter.HandleFunc("/registry_prefix", handler.GetRegistryPrefix).Methods("GET")
	authRouter.HandleFunc("/image_exists", handler.CheckImageExists).Methods("GET")

	if cfg.ProxyBaseURL != "" {
		authRouter.PathPrefix("/sandbox/").HandlerFunc(handler.ProxySandbox)
		logger.Info("Proxy mode enabled: sandbox URLs under %s/sandbox/{runtime_id}", cfg.ProxyBaseURL)
	}

	// Start server with timeouts
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	logger.Info("Starting OpenHands Kubernetes Runtime API server on %s", addr)
	logger.Info("Namespace: %s", cfg.Namespace)
	logger.Info("Base Domain: %s", cfg.BaseDomain)
	if cfg.ProxyBaseURL != "" {
		logger.Info("Proxy Base URL: %s (ephemeral sandbox traffic via runtime API)", cfg.ProxyBaseURL)
	}
	logger.Info("Registry Prefix: %s", cfg.RegistryPrefix)
	logger.Debug("Agent Server Port: %d", cfg.AgentServerPort)
	logger.Debug("VSCode Port: %d", cfg.VSCodePort)
	logger.Debug("Worker 1 Port: %d", cfg.Worker1Port)
	logger.Debug("Worker 2 Port: %d", cfg.Worker2Port)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run server in a goroutine so it doesn't block
	go func() {
		logger.Info("HTTP server starting...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Set up channel to listen for interrupt or terminate signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Block until we receive a signal
	sig := <-quit
	logger.Info("Received shutdown signal: %v", sig)
	logger.Info("Gracefully shutting down server...")

	// Stop the reaper
	reaperInstance.Stop()

	// Create a context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Attempt graceful shutdown
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Info("Server forced to shutdown: %v", err)
		os.Exit(1)
	}

	logger.Info("Server shutdown complete")
}
