package utils

import (
	"os"
	"testing"

	interfaces "github.com/llm-d-incubation/workload-variant-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	"github.com/stretchr/testify/assert"
)

func init() {
	// Initialize logger for tests
	_, err := logger.InitLogger()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

func TestCreateTLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		promConfig  *interfaces.PrometheusConfig
		expectError bool
	}{
		{
			name:        "nil config",
			promConfig:  nil,
			expectError: false,
		},
		{
			name: "TLS with insecure skip verify",
			promConfig: &interfaces.PrometheusConfig{
				InsecureSkipVerify: true,
			},
			expectError: false,
		},
		{
			name: "TLS with server name",
			promConfig: &interfaces.PrometheusConfig{
				ServerName: "prometheus.example.com",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := CreateTLSConfig(tt.promConfig)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if tt.promConfig != nil {
				assert.NotNil(t, config)
			} else {
				assert.Nil(t, config)
			}
		})
	}
}

func TestParsePrometheusConfigFromEnv(t *testing.T) {
	// Test with HTTPS URL (default)
	if err := os.Setenv("PROMETHEUS_BASE_URL", "https://prometheus:9090"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	config := ParsePrometheusConfigFromEnv()
	assert.Equal(t, "https://prometheus:9090", config.BaseURL)

	// Test with explicit TLS configuration
	if err := os.Setenv("PROMETHEUS_BASE_URL", "https://prometheus:9090"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_TLS_INSECURE_SKIP_VERIFY", "true"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	config = ParsePrometheusConfigFromEnv()
	assert.Equal(t, "https://prometheus:9090", config.BaseURL)
	assert.True(t, config.InsecureSkipVerify)

	// Test OpenShift configuration
	if err := os.Setenv("PROMETHEUS_BASE_URL", "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_TLS_INSECURE_SKIP_VERIFY", "false"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_CA_CERT_PATH", "/etc/openshift-ca/ca.crt"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_CLIENT_CERT_PATH", ""); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_CLIENT_KEY_PATH", ""); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_SERVER_NAME", "thanos-querier.openshift-monitoring.svc"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("PROMETHEUS_TOKEN_PATH", "/var/run/secrets/kubernetes.io/serviceaccount/token"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	config = ParsePrometheusConfigFromEnv()
	assert.Equal(t, "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091", config.BaseURL)
	assert.False(t, config.InsecureSkipVerify)
	assert.Equal(t, "/etc/openshift-ca/ca.crt", config.CACertPath)
	assert.Equal(t, "", config.ClientCertPath)
	assert.Equal(t, "", config.ClientKeyPath)
	assert.Equal(t, "thanos-querier.openshift-monitoring.svc", config.ServerName)
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", config.TokenPath)

	// Clean up
	if err := os.Unsetenv("PROMETHEUS_BASE_URL"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv("PROMETHEUS_TLS_INSECURE_SKIP_VERIFY"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv("PROMETHEUS_CA_CERT_PATH"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv("PROMETHEUS_CLIENT_CERT_PATH"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv("PROMETHEUS_CLIENT_KEY_PATH"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv("PROMETHEUS_SERVER_NAME"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv("PROMETHEUS_TOKEN_PATH"); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
}

func TestValidateTLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		promConfig  *interfaces.PrometheusConfig
		expectError bool
		expectPanic bool
	}{
		{
			name:        "nil config - should panic",
			promConfig:  nil,
			expectError: false,
			expectPanic: true,
		},
		{
			name: "HTTP URL - should fail",
			promConfig: &interfaces.PrometheusConfig{
				BaseURL: "http://prometheus:9090",
			},
			expectError: true,
			expectPanic: false,
		},
		{
			name: "TLS with insecure skip verify",
			promConfig: &interfaces.PrometheusConfig{
				InsecureSkipVerify: true,
				BaseURL:            "https://prometheus:9090",
			},
			expectError: false,
			expectPanic: false,
		},
		{
			name: "TLS with server name",
			promConfig: &interfaces.PrometheusConfig{
				ServerName: "prometheus.example.com",
				BaseURL:    "https://prometheus:9090",
			},
			expectError: false,
			expectPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				assert.Panics(t, func() {
					_ = ValidateTLSConfig(tt.promConfig)
				})
			} else {
				err := ValidateTLSConfig(tt.promConfig)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
		})
	}
}
