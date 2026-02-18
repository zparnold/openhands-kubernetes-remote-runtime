package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Server configuration
	ServerPort      string
	APIKey          string
	LogLevel        string
	ShutdownTimeout time.Duration

	// Kubernetes operation timeouts
	K8sOperationTimeout time.Duration // Timeout for create/delete operations (pods, services, ingresses)
	K8sQueryTimeout     time.Duration // Timeout for get/list operations

	// Kubernetes configuration
	Namespace    string
	IngressClass string
	BaseDomain   string

	// Sandbox ingress: optional annotations added to each sandbox Ingress (e.g. cert-manager, TLS)
	// Set via SANDBOX_INGRESS_ANNOTATIONS as comma-separated key=value pairs.
	SandboxIngressAnnotations map[string]string

	// Container configuration
	RegistryPrefix   string
	DefaultImage     string
	ImagePullSecrets []string // Kubernetes secret names for pulling sandbox images (e.g. private registry)

	// Pod configuration
	AgentServerPort int
	VSCodePort      int
	Worker1Port     int
	Worker2Port     int

	// App server configuration
	AppServerURL       string
	AppServerPublicURL string

	// Proxy mode: when set, /start returns URLs under this base (e.g. https://runtime-api.example.com)
	// so sandbox traffic goes through this API instead of per-sandbox DNS. Avoids DNS propagation delay.
	ProxyBaseURL string

	// Cleanup configuration
	CleanupEnabled            bool // Enable automatic cleanup of orphaned resources
	CleanupIntervalMinutes    int  // Interval between cleanup runs (in minutes)
	CleanupFailedThresholdMin int  // Time before cleaning up failed pods (in minutes)
	CleanupIdleThresholdMin   int  // Time before cleaning up idle pods (in minutes)

	// Optional CA certificate for sandbox pods. When set, the secret is mounted into each sandbox
	// at /usr/local/share/ca-certificates/additional-ca.crt. The runtime image runs update-ca-certificates
	// at startup, which merges these certs into the system trust store (for corporate/proxy CAs).
	CACertSecretName string // Kubernetes secret name (e.g. "ca-certificates")
	CACertSecretKey  string // Key within the secret (default "ca-certificates.crt")
}

func LoadConfig() *Config {
	return &Config{
		ServerPort:                getEnv("SERVER_PORT", "8080"),
		APIKey:                    getEnv("API_KEY", ""),
		LogLevel:                  getEnv("LOG_LEVEL", "info"),
		ShutdownTimeout:           getEnvAsDuration("SHUTDOWN_TIMEOUT", 30*time.Second),
		K8sOperationTimeout:       getEnvAsDuration("K8S_OPERATION_TIMEOUT", 60*time.Second),
		K8sQueryTimeout:           getEnvAsDuration("K8S_QUERY_TIMEOUT", 10*time.Second),
		Namespace:                 getEnv("NAMESPACE", "openhands"),
		IngressClass:              getEnv("INGRESS_CLASS", "nginx"),
		BaseDomain:                getEnv("BASE_DOMAIN", "sandbox.example.com"),
		SandboxIngressAnnotations: parseAnnotations(getEnv("SANDBOX_INGRESS_ANNOTATIONS", "")),
		RegistryPrefix:            getEnv("REGISTRY_PREFIX", "ghcr.io/openhands"),
		DefaultImage:              getEnv("DEFAULT_IMAGE", "ghcr.io/openhands/runtime:latest"),
		ImagePullSecrets:          parseSecretNames(getEnv("IMAGE_PULL_SECRETS", "")),
		AgentServerPort:           getEnvAsInt("AGENT_SERVER_PORT", 60000),
		VSCodePort:                getEnvAsInt("VSCODE_PORT", 60001),
		Worker1Port:               getEnvAsInt("WORKER_1_PORT", 12000),
		Worker2Port:               getEnvAsInt("WORKER_2_PORT", 12001),
		AppServerURL:              getEnv("APP_SERVER_URL", ""),
		AppServerPublicURL:        getEnv("APP_SERVER_PUBLIC_URL", ""),
		ProxyBaseURL:              strings.TrimSuffix(getEnv("PROXY_BASE_URL", ""), "/"),
		CleanupEnabled:            getEnvAsBool("CLEANUP_ENABLED", true),
		CleanupIntervalMinutes:    getEnvAsInt("CLEANUP_INTERVAL_MINUTES", 5),
		CleanupFailedThresholdMin: getEnvAsInt("CLEANUP_FAILED_THRESHOLD_MINUTES", 60),
		CleanupIdleThresholdMin:   getEnvAsInt("CLEANUP_IDLE_THRESHOLD_MINUTES", 1440), // 24 hours
		CACertSecretName:          getEnv("CA_CERT_SECRET_NAME", ""),
		CACertSecretKey:           getEnv("CA_CERT_SECRET_KEY", "ca-certificates.crt"),
	}
}

// parseAnnotations parses "key1=value1,key2=value2" into a map. Values may contain "=".
func parseAnnotations(s string) map[string]string {
	out := make(map[string]string)
	if s == "" {
		return out
	}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}

// parseSecretNames parses a comma-separated list of Kubernetes secret names (e.g. for imagePullSecrets).
func parseSecretNames(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func getEnv(key, defaultVal string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvAsBool(key string, defaultVal bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

func getEnvAsDuration(key string, defaultVal time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultVal
}
