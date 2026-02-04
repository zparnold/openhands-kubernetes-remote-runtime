package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Server configuration
	ServerPort     string
	APIKey         string
	LogLevel       string
	
	// Kubernetes configuration
	Namespace      string
	IngressClass   string
	BaseDomain     string
	
	// Container configuration
	RegistryPrefix string
	DefaultImage   string
	
	// Pod configuration
	AgentServerPort  int
	VSCodePort       int
	Worker1Port      int
	Worker2Port      int
	
	// App server configuration
	AppServerURL    string
	AppServerPublicURL string
}

func LoadConfig() *Config {
	return &Config{
		ServerPort:         getEnv("SERVER_PORT", "8080"),
		APIKey:             getEnv("API_KEY", ""),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		Namespace:          getEnv("NAMESPACE", "openhands"),
		IngressClass:       getEnv("INGRESS_CLASS", "nginx"),
		BaseDomain:         getEnv("BASE_DOMAIN", "sandbox.example.com"),
		RegistryPrefix:     getEnv("REGISTRY_PREFIX", "ghcr.io/openhands"),
		DefaultImage:       getEnv("DEFAULT_IMAGE", "ghcr.io/openhands/runtime:latest"),
		AgentServerPort:    getEnvAsInt("AGENT_SERVER_PORT", 60000),
		VSCodePort:         getEnvAsInt("VSCODE_PORT", 60001),
		Worker1Port:        getEnvAsInt("WORKER_1_PORT", 12000),
		Worker2Port:        getEnvAsInt("WORKER_2_PORT", 12001),
		AppServerURL:       getEnv("APP_SERVER_URL", ""),
		AppServerPublicURL: getEnv("APP_SERVER_PUBLIC_URL", ""),
	}
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
