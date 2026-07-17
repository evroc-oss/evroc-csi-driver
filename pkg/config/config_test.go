// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkconfig "github.com/evroc-oss/evroc-go-sdk/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempConfig writes the given YAML content to a temporary file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

// validAuthAndContext returns a minimal YAML snippet with auth and context so that
// sdkconfig.Validate() succeeds.
func validAuthAndContext() string {
	return `
api:
  base_url: "https://api.example.com"
context:
  project: "my-project"
  region: "us-east-1"
auth:
  token: "my-token"
`
}

func TestLoadFromPath_Success(t *testing.T) {
	yaml := validAuthAndContext() + `
csi:
  identifier: "csi-01"
  maxVolumesPerNode: 64
  totalCapacityGB: 500
  deviceScanTimeout: 45s
  deviceScanInterval: 3s
  attachmentPollTimeout: 5m
  metricsCollectionInterval: 15s
  restAPIMaxRetries: 5
  restAPIRetryInitialDelay: 2s
  restAPIRetryMaxDelay: 20s
  restAPITimeout: 60s
`
	path := writeTempConfig(t, yaml)

	cfg, err := LoadFromPath(path)
	require.NoError(t, err)
	assert.Equal(t, "csi-01", cfg.CSI.Identifier)
	assert.Equal(t, int64(64), cfg.CSI.MaxVolumesPerNode)
	assert.Equal(t, int64(500), cfg.CSI.TotalCapacityGB)
	assert.Equal(t, 45*time.Second, cfg.CSI.DeviceScanTimeout)
	assert.Equal(t, 3*time.Second, cfg.CSI.DeviceScanInterval)
	assert.Equal(t, 5*time.Minute, cfg.CSI.AttachmentPollTimeout)
	assert.Equal(t, 15*time.Second, cfg.CSI.MetricsCollectionInterval)
	assert.Equal(t, 5, cfg.CSI.RestAPIMaxRetries)
	assert.Equal(t, 2*time.Second, cfg.CSI.RestAPIRetryInitialDelay)
	assert.Equal(t, 20*time.Second, cfg.CSI.RestAPIRetryMaxDelay)
	assert.Equal(t, 60*time.Second, cfg.CSI.RestAPITimeout)
}

func TestLoadFromPath_FileNotFound(t *testing.T) {
	_, err := LoadFromPath("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadFromPath_InvalidYAML(t *testing.T) {
	yaml := `
api:
  base_url: [invalid
`
	path := writeTempConfig(t, yaml)

	_, err := LoadFromPath(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML config")
}

func TestLoadFromPath_ValidationFails(t *testing.T) {
	// Missing required auth and context
	yaml := `
csi:
  identifier: "csi-01"
`
	path := writeTempConfig(t, yaml)

	_, err := LoadFromPath(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication required")
}

func TestLoadFromPath_InvalidIdentifier(t *testing.T) {
	yaml := validAuthAndContext() + `
csi:
  identifier: "invalid id!"
`
	path := writeTempConfig(t, yaml)

	_, err := LoadFromPath(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "csi.identifier must contain only alphanumeric characters")
}

func TestNewWithDefaults(t *testing.T) {
	cfg := NewWithDefaults()
	require.NotNil(t, cfg)

	assert.Equal(t, int64(DefaultMaxVolumesPerNode), cfg.CSI.MaxVolumesPerNode)
	assert.Equal(t, int64(DefaultTotalCapacityGB), cfg.CSI.TotalCapacityGB)
	assert.Equal(t, DefaultDeviceScanTimeout, cfg.CSI.DeviceScanTimeout)
	assert.Equal(t, DefaultDeviceScanInterval, cfg.CSI.DeviceScanInterval)
	assert.Equal(t, DefaultAttachmentPollTimeout, cfg.CSI.AttachmentPollTimeout)
	assert.Equal(t, DefaultMetricsCollectionInterval, cfg.CSI.MetricsCollectionInterval)
	assert.Equal(t, DefaultRestAPIMaxRetries, cfg.CSI.RestAPIMaxRetries)
	assert.Equal(t, DefaultRestAPIRetryInitialDelay, cfg.CSI.RestAPIRetryInitialDelay)
	assert.Equal(t, DefaultRestAPIRetryMaxDelay, cfg.CSI.RestAPIRetryMaxDelay)
	assert.Equal(t, DefaultRestAPITimeout, cfg.CSI.RestAPITimeout)
}

func TestApplyDefaults_ZeroValues(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	assert.Equal(t, int64(DefaultMaxVolumesPerNode), cfg.CSI.MaxVolumesPerNode)
	assert.Equal(t, int64(DefaultTotalCapacityGB), cfg.CSI.TotalCapacityGB)
	assert.Equal(t, DefaultDeviceScanTimeout, cfg.CSI.DeviceScanTimeout)
	assert.Equal(t, DefaultDeviceScanInterval, cfg.CSI.DeviceScanInterval)
	assert.Equal(t, DefaultAttachmentPollTimeout, cfg.CSI.AttachmentPollTimeout)
	assert.Equal(t, DefaultMetricsCollectionInterval, cfg.CSI.MetricsCollectionInterval)
	assert.Equal(t, DefaultRestAPIMaxRetries, cfg.CSI.RestAPIMaxRetries)
	assert.Equal(t, DefaultRestAPIRetryInitialDelay, cfg.CSI.RestAPIRetryInitialDelay)
	assert.Equal(t, DefaultRestAPIRetryMaxDelay, cfg.CSI.RestAPIRetryMaxDelay)
	assert.Equal(t, DefaultRestAPITimeout, cfg.CSI.RestAPITimeout)
}

func TestApplyDefaults_PreservesExplicitValues(t *testing.T) {
	cfg := &Config{
		CSI: CSIConfig{
			MaxVolumesPerNode:         99,
			TotalCapacityGB:           200,
			DeviceScanTimeout:         10 * time.Second,
			DeviceScanInterval:        1 * time.Second,
			AttachmentPollTimeout:     1 * time.Minute,
			MetricsCollectionInterval: 5 * time.Second,
			RestAPIMaxRetries:         1,
			RestAPIRetryInitialDelay:  500 * time.Millisecond,
			RestAPIRetryMaxDelay:      5 * time.Second,
			RestAPITimeout:            15 * time.Second,
		},
	}
	cfg.applyDefaults()

	assert.Equal(t, int64(99), cfg.CSI.MaxVolumesPerNode)
	assert.Equal(t, int64(200), cfg.CSI.TotalCapacityGB)
	assert.Equal(t, 10*time.Second, cfg.CSI.DeviceScanTimeout)
	assert.Equal(t, 1*time.Second, cfg.CSI.DeviceScanInterval)
	assert.Equal(t, 1*time.Minute, cfg.CSI.AttachmentPollTimeout)
	assert.Equal(t, 5*time.Second, cfg.CSI.MetricsCollectionInterval)
	assert.Equal(t, 1, cfg.CSI.RestAPIMaxRetries)
	assert.Equal(t, 500*time.Millisecond, cfg.CSI.RestAPIRetryInitialDelay)
	assert.Equal(t, 5*time.Second, cfg.CSI.RestAPIRetryMaxDelay)
	assert.Equal(t, 15*time.Second, cfg.CSI.RestAPITimeout)
}

func TestApplyDefaults_NegativeMaxVolumesMeansUnlimited(t *testing.T) {
	cfg := &Config{
		CSI: CSIConfig{
			MaxVolumesPerNode: -1,
		},
	}
	cfg.applyDefaults()
	assert.Equal(t, int64(-1), cfg.CSI.MaxVolumesPerNode)
}

func TestApplyDefaults_NegativeTotalCapacityDefaultsOverridden(t *testing.T) {
	// TotalCapacityGB uses <= 0, so negative gets overridden
	cfg := &Config{
		CSI: CSIConfig{
			TotalCapacityGB: -5,
		},
	}
	cfg.applyDefaults()
	assert.Equal(t, int64(DefaultTotalCapacityGB), cfg.CSI.TotalCapacityGB)
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Auth: sdkconfig.AuthConfig{Token: "token"},
		API:  sdkconfig.APIConfig{BaseURL: "https://api.example.com"},
		Context: sdkconfig.ContextConfig{
			Project: "proj",
			Region:  "reg",
		},
	}
	cfg.applyDefaults()
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_InvalidIdentifier(t *testing.T) {
	cfg := &Config{
		Auth: sdkconfig.AuthConfig{Token: "token"},
		Context: sdkconfig.ContextConfig{
			Project: "proj",
			Region:  "reg",
		},
		CSI: CSIConfig{
			Identifier: "bad identifier!",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "csi.identifier must contain only alphanumeric characters")
}

func TestValidate_EmptyIdentifierAllowed(t *testing.T) {
	cfg := &Config{
		Auth: sdkconfig.AuthConfig{Token: "token"},
		Context: sdkconfig.ContextConfig{
			Project: "proj",
			Region:  "reg",
		},
		CSI: CSIConfig{
			Identifier: "",
		},
	}
	cfg.applyDefaults()
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestString_Redaction(t *testing.T) {
	cfg := &Config{
		Auth: sdkconfig.AuthConfig{
			TokenURL:             "https://auth.example.com",
			ClientID:             "client-id",
			Username:             "user",
			Password:             "secret-password",
			Token:                "secret-token",
			RefreshToken:         "secret-refresh",
			Scopes:               []string{"openid"},
			ServiceAccountID:     "sa-123",
			ServiceAccountSecret: "secret-sa",
		},
		Evroc: EvrocConfig{
			RestURL:      "https://api.evroc.com",
			Organization: "my-org",
			Project:      "my-proj",
		},
		CSI: CSIConfig{
			Identifier: "csi-node-1",
		},
		API: sdkconfig.APIConfig{
			BaseURL: "https://api.example.com",
		},
		Context: sdkconfig.ContextConfig{
			Organization: "ctx-org",
			Project:      "ctx-proj",
			Region:       "ctx-region",
		},
		Infrastructure: InfrastructureConfig{
			Region: "infra-region",
		},
	}

	out := cfg.String()
	assert.Contains(t, out, "Password: <redacted")
	assert.Contains(t, out, "Token: <redacted")
	assert.Contains(t, out, "RefreshToken: <redacted")
	assert.Contains(t, out, "ServiceAccountSecret: <redacted")
	assert.Contains(t, out, "client-id")
	assert.Contains(t, out, "csi-node-1")
}

func TestSDKConfig_MapsFieldsDirectly(t *testing.T) {
	cfg := &Config{
		Auth: sdkconfig.AuthConfig{Token: "my-token"},
		API:  sdkconfig.APIConfig{BaseURL: "https://api.example.com"},
		Context: sdkconfig.ContextConfig{
			Project:      "my-project",
			Region:       "us-east-1",
			Organization: "my-org",
		},
	}

	sdk := cfg.SDKConfig()
	assert.Equal(t, "my-token", sdk.Auth.Token)
	assert.Equal(t, "https://api.example.com", sdk.API.BaseURL)
	assert.Equal(t, "my-project", sdk.Context.Project)
	assert.Equal(t, "us-east-1", sdk.Context.Region)
	assert.Equal(t, "my-org", sdk.Context.Organization)
}

func TestSDKConfig_FallsBackToDeprecatedEvrocFields(t *testing.T) {
	cfg := &Config{
		Evroc: EvrocConfig{
			RestURL:      "https://evroc.example.com",
			Organization: "evroc-org",
			Project:      "evroc-proj",
		},
		Infrastructure: InfrastructureConfig{
			Region: "evroc-region",
		},
	}

	sdk := cfg.SDKConfig()
	assert.Equal(t, "https://evroc.example.com", sdk.API.BaseURL)
	assert.Equal(t, "evroc-org", sdk.Context.Organization)
	assert.Equal(t, "evroc-proj", sdk.Context.Project)
	assert.Equal(t, "evroc-region", sdk.Context.Region)
}

func TestSDKConfig_NewFieldsTakePrecedenceOverDeprecated(t *testing.T) {
	cfg := &Config{
		Evroc: EvrocConfig{
			RestURL:      "https://evroc.example.com",
			Organization: "evroc-org",
			Project:      "evroc-proj",
		},
		Infrastructure: InfrastructureConfig{
			Region: "evroc-region",
		},
		API: sdkconfig.APIConfig{
			BaseURL: "https://api.example.com",
		},
		Context: sdkconfig.ContextConfig{
			Organization: "ctx-org",
			Project:      "ctx-proj",
			Region:       "ctx-region",
		},
	}

	sdk := cfg.SDKConfig()
	assert.Equal(t, "https://api.example.com", sdk.API.BaseURL)
	assert.Equal(t, "ctx-org", sdk.Context.Organization)
	assert.Equal(t, "ctx-proj", sdk.Context.Project)
	assert.Equal(t, "ctx-region", sdk.Context.Region)
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected bool
	}{
		{"alphanumeric", "abc123", true},
		{"with hyphens", "my-id-123", true},
		{"with underscores", "my_id_123", true},
		{"mixed", "CSI-Driver_v1", true},
		{"empty", "", false},
		{"with space", "bad id", false},
		{"with special chars", "id!@#", false},
		{"with dot", "my.id", false},
		{"with slash", "my/id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidIdentifier(tt.id))
		})
	}
}
