// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// validConfig returns a valid configuration YAML for testing
func validConfig() string {
	return `evroc:
  organization: test-org
  project: test-workspace
auth:
  username: testuser
  password: testpass
`
}

// writeConfigFile writes a YAML config to a temp directory and returns the full file path
func writeConfigFile(t *testing.T, configYAML string) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ConfigFileName)
	err := os.WriteFile(configPath, []byte(configYAML), 0o600)
	require.NoError(t, err)
	return configPath
}

func TestLoadFromPath(t *testing.T) {
	configPath := writeConfigFile(t, `evroc:
  restURL: https://api.cloud.evroc.com
  organization: test-org
  project: test-workspace
auth:
  issuerURL: https://authn.iam.evroc.com/realms/evroc-customer
  clientID: csi-driver
  username: testuser
  password: testpass
infrastructure:
  region: se-sto
csi:
  identifier: test-id
`)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)
	require.Equal(t, "https://api.cloud.evroc.com", cfg.Evroc.RestURL)
	require.Equal(t, "test-org", cfg.Evroc.Organization)
	require.Equal(t, "test-workspace", cfg.Evroc.Project)
	require.Equal(t, "https://authn.iam.evroc.com/realms/evroc-customer", cfg.Auth.IssuerURL)
	require.Equal(t, "csi-driver", cfg.Auth.ClientID)
	require.Equal(t, "testuser", cfg.Auth.Username)
	require.Equal(t, "testpass", cfg.Auth.Password)
	require.Equal(t, "se-sto", cfg.Infrastructure.Region)
	require.Equal(t, "test-id", cfg.CSI.Identifier)
}

func TestLoadFromPathMissingRequired(t *testing.T) {
	tests := []struct {
		name    string
		omitKey string
		errMsg  string
	}{
		{"missing organization", "organization", "evroc.organization is required"},
		{"missing project", "project", "evroc.project is required"},
		{"missing username", "username", "auth.username is required"},
		{"missing password", "password", "auth.password is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Evroc.Organization = "test-org"
			cfg.Evroc.Project = "test-workspace"
			cfg.Auth.Username = "testuser"
			cfg.Auth.Password = "testpass"

			// Remove the field being tested
			switch tt.omitKey {
			case "organization":
				cfg.Evroc.Organization = ""
			case "project":
				cfg.Evroc.Project = ""
			case "username":
				cfg.Auth.Username = ""
			case "password":
				cfg.Auth.Password = ""
			}

			cfg.applyDefaults()
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestLoadFromPathWithDefaults(t *testing.T) {
	// Minimal config - should apply defaults
	configPath := writeConfigFile(t, validConfig())

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)
	require.Equal(t, DefaultRestURL, cfg.Evroc.RestURL)
	require.Equal(t, DefaultAuthIssuerURL, cfg.Auth.IssuerURL)
	require.Equal(t, DefaultAuthClientID, cfg.Auth.ClientID)
}

func TestNewWithDefaults(t *testing.T) {
	cfg := NewWithDefaults()
	require.NotNil(t, cfg)

	// Verify CSI defaults are applied
	require.Equal(t, int64(DefaultMaxVolumesPerNode), cfg.CSI.MaxVolumesPerNode)
	require.Equal(t, int64(DefaultTotalCapacityGB), cfg.CSI.TotalCapacityGB)
	require.Equal(t, DefaultDeviceScanTimeout, cfg.CSI.DeviceScanTimeout)
	require.Equal(t, DefaultDeviceScanInterval, cfg.CSI.DeviceScanInterval)
	require.Equal(t, DefaultAttachmentPollTimeout, cfg.CSI.AttachmentPollTimeout)
	require.Equal(t, DefaultMetricsCollectionInterval, cfg.CSI.MetricsCollectionInterval)
	require.Equal(t, DefaultRestAPIMaxRetries, cfg.CSI.RestAPIMaxRetries)
	require.Equal(t, DefaultRestAPIRetryInitialDelay, cfg.CSI.RestAPIRetryInitialDelay)
	require.Equal(t, DefaultRestAPIRetryMaxDelay, cfg.CSI.RestAPIRetryMaxDelay)
	require.Equal(t, DefaultRestAPITimeout, cfg.CSI.RestAPITimeout)

	// Verify platform defaults are applied
	require.Equal(t, DefaultRestURL, cfg.Evroc.RestURL)
	require.Equal(t, DefaultAuthIssuerURL, cfg.Auth.IssuerURL)
	require.Equal(t, DefaultAuthClientID, cfg.Auth.ClientID)
}

func TestValidate(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			Evroc: EvrocConfig{
				RestURL:      "https://api.cloud.evroc.com",
				Organization: "test-org",
				Project:      "test-workspace",
			},
			Auth: AuthConfig{
				IssuerURL: "https://authn.iam.evroc.com/realms/evroc-customer",
				ClientID:  "csi-driver",
				Username:  "testuser",
				Password:  "testpass",
			},
			Infrastructure: InfrastructureConfig{
				Region: "se-sto",
			},
			CSI: CSIConfig{
				Identifier: "test-id",
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"valid config", func(c *Config) {}, ""},
		{"valid without region", func(c *Config) { c.Infrastructure.Region = "" }, ""},
		{"invalid REST URL", func(c *Config) { c.Evroc.RestURL = "not-a-url" }, "HTTP or HTTPS URL"},
		{"invalid REST URL - non-HTTP", func(c *Config) { c.Evroc.RestURL = "ftp://example.com" }, "HTTP or HTTPS URL"},
		{"invalid project", func(c *Config) { c.Evroc.Project = "invalid@name" }, "alphanumeric"},
		{"valid identifier", func(c *Config) { c.CSI.Identifier = "valid-id_123" }, ""},
		{"invalid identifier", func(c *Config) { c.CSI.Identifier = "invalid@id" }, "alphanumeric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(cfg)
			err := cfg.Validate()

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
