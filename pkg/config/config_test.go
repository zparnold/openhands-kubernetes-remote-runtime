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

func TestParseAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{"Empty string", "", map[string]string{}},
		{"Single pair", "key1=value1", map[string]string{"key1": "value1"}},
		{"Multiple pairs", "k1=v1,k2=v2", map[string]string{"k1": "v1", "k2": "v2"}},
		{"Value with equals", "cert-manager.io/issuer=step-issuer-name", map[string]string{"cert-manager.io/issuer": "step-issuer-name"}},
		{"With spaces", " k1 = v1 , k2 = v2 ", map[string]string{"k1": "v1", "k2": "v2"}},
		{"Skip empty pair", "k1=v1,,k2=v2", map[string]string{"k1": "v1", "k2": "v2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAnnotations(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("Expected %d entries, got %d: %v", len(tt.expected), len(got), got)
				return
			}
			for k, v := range tt.expected {
				if got[k] != v {
					t.Errorf("Key %q: expected %q, got %q", k, v, got[k])
				}
			}
		})
	}
}

func TestParseSecretNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"Empty string", "", nil},
		{"Single secret", "my-registry-secret", []string{"my-registry-secret"}},
		{"Multiple secrets", "secret1,secret2", []string{"secret1", "secret2"}},
		{"With spaces", " s1 , s2 ", []string{"s1", "s2"}},
		{"Skip empty", "s1,,s2", []string{"s1", "s2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSecretNames(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, got)
				return
			}
			for i := range tt.expected {
				if got[i] != tt.expected[i] {
					t.Errorf("Index %d: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestLoadConfig_ImagePullSecrets(t *testing.T) {
	orig := os.Getenv("IMAGE_PULL_SECRETS")
	defer func() {
		if orig == "" {
			os.Unsetenv("IMAGE_PULL_SECRETS")
		} else {
			os.Setenv("IMAGE_PULL_SECRETS", orig)
		}
	}()

	os.Setenv("IMAGE_PULL_SECRETS", "my-registry-secret,other-secret")
	cfg := LoadConfig()
	if len(cfg.ImagePullSecrets) != 2 {
		t.Fatalf("Expected 2 ImagePullSecrets, got %d: %v", len(cfg.ImagePullSecrets), cfg.ImagePullSecrets)
	}
	if cfg.ImagePullSecrets[0] != "my-registry-secret" || cfg.ImagePullSecrets[1] != "other-secret" {
		t.Errorf("Expected [my-registry-secret other-secret], got %v", cfg.ImagePullSecrets)
	}

	os.Unsetenv("IMAGE_PULL_SECRETS")
	cfg = LoadConfig()
	if cfg.ImagePullSecrets != nil {
		t.Errorf("Expected nil ImagePullSecrets when unset, got %v", cfg.ImagePullSecrets)
	}
}

func TestLoadConfig_ProxyBaseURL(t *testing.T) {
	orig := os.Getenv("PROXY_BASE_URL")
	defer func() {
		if orig == "" {
			os.Unsetenv("PROXY_BASE_URL")
		} else {
			os.Setenv("PROXY_BASE_URL", orig)
		}
	}()

	t.Run("Empty when unset", func(t *testing.T) {
		os.Unsetenv("PROXY_BASE_URL")
		cfg := LoadConfig()
		if cfg.ProxyBaseURL != "" {
			t.Errorf("Expected empty ProxyBaseURL when unset, got %q", cfg.ProxyBaseURL)
		}
	})

	t.Run("Loaded and trailing slash trimmed", func(t *testing.T) {
		os.Setenv("PROXY_BASE_URL", "https://runtime-api.example.com/")
		cfg := LoadConfig()
		if cfg.ProxyBaseURL != "https://runtime-api.example.com" {
			t.Errorf("Expected trailing slash trimmed, got %q", cfg.ProxyBaseURL)
		}
	})

	t.Run("Loaded without trailing slash", func(t *testing.T) {
		os.Setenv("PROXY_BASE_URL", "https://runtime-api.example.com")
		cfg := LoadConfig()
		if cfg.ProxyBaseURL != "https://runtime-api.example.com" {
			t.Errorf("Expected same value, got %q", cfg.ProxyBaseURL)
		}
	})
}
