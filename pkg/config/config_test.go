package config

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Save original env vars
	origEnv := map[string]string{
		"SERVER_PORT":       os.Getenv("SERVER_PORT"),
		"API_KEY":           os.Getenv("API_KEY"),
		"NAMESPACE":         os.Getenv("NAMESPACE"),
		"BASE_DOMAIN":       os.Getenv("BASE_DOMAIN"),
		"REGISTRY_PREFIX":   os.Getenv("REGISTRY_PREFIX"),
		"AGENT_SERVER_PORT": os.Getenv("AGENT_SERVER_PORT"),
	}

	// Restore env vars after test
	defer func() {
		for k, v := range origEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	t.Run("Load default config", func(t *testing.T) {
		// Clear all env vars
		for k := range origEnv {
			os.Unsetenv(k)
		}

		cfg := LoadConfig()

		if cfg.ServerPort != "8080" {
			t.Errorf("Expected default ServerPort '8080', got '%s'", cfg.ServerPort)
		}
		if cfg.Namespace != "openhands" {
			t.Errorf("Expected default Namespace 'openhands', got '%s'", cfg.Namespace)
		}
		if cfg.BaseDomain != "sandbox.example.com" {
			t.Errorf("Expected default BaseDomain 'sandbox.example.com', got '%s'", cfg.BaseDomain)
		}
		if cfg.AgentServerPort != 60000 {
			t.Errorf("Expected default AgentServerPort 60000, got %d", cfg.AgentServerPort)
		}
		if cfg.VSCodePort != 60001 {
			t.Errorf("Expected default VSCodePort 60001, got %d", cfg.VSCodePort)
		}
		if cfg.Worker1Port != 12000 {
			t.Errorf("Expected default Worker1Port 12000, got %d", cfg.Worker1Port)
		}
		if cfg.Worker2Port != 12001 {
			t.Errorf("Expected default Worker2Port 12001, got %d", cfg.Worker2Port)
		}
	})

	t.Run("Load config from environment", func(t *testing.T) {
		os.Setenv("SERVER_PORT", "9090")
		os.Setenv("API_KEY", "test-api-key")
		os.Setenv("NAMESPACE", "test-namespace")
		os.Setenv("BASE_DOMAIN", "test.example.com")
		os.Setenv("REGISTRY_PREFIX", "test-registry/prefix")
		os.Setenv("AGENT_SERVER_PORT", "50000")

		cfg := LoadConfig()

		if cfg.ServerPort != "9090" {
			t.Errorf("Expected ServerPort '9090', got '%s'", cfg.ServerPort)
		}
		if cfg.APIKey != "test-api-key" {
			t.Errorf("Expected APIKey 'test-api-key', got '%s'", cfg.APIKey)
		}
		if cfg.Namespace != "test-namespace" {
			t.Errorf("Expected Namespace 'test-namespace', got '%s'", cfg.Namespace)
		}
		if cfg.BaseDomain != "test.example.com" {
			t.Errorf("Expected BaseDomain 'test.example.com', got '%s'", cfg.BaseDomain)
		}
		if cfg.RegistryPrefix != "test-registry/prefix" {
			t.Errorf("Expected RegistryPrefix 'test-registry/prefix', got '%s'", cfg.RegistryPrefix)
		}
		if cfg.AgentServerPort != 50000 {
			t.Errorf("Expected AgentServerPort 50000, got %d", cfg.AgentServerPort)
		}
	})
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		defaultVal string
		envValue   string
		expected   string
	}{
		{"Use default when env not set", "TEST_KEY_1", "default", "", "default"},
		{"Use env value when set", "TEST_KEY_2", "default", "custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnv(tt.key, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestGetEnvAsInt(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		defaultVal int
		envValue   string
		expected   int
	}{
		{"Use default when env not set", "TEST_INT_1", 100, "", 100},
		{"Use env value when set", "TEST_INT_2", 100, "200", 200},
		{"Use default when env is invalid", "TEST_INT_3", 100, "invalid", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnvAsInt(tt.key, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}
