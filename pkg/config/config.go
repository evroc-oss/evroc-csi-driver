// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	sdkconfig "github.com/evroc-oss/evroc-go-sdk/config"
	"gopkg.in/yaml.v3"
)

const (
	// ConfigPath is the path where configuration files are mounted.
	ConfigPath = "/etc/evroc-csi"

	// ConfigFileName is the name of the YAML configuration file.
	ConfigFileName = "config.yaml"

	// DefaultRestURL is the default production REST API server URL.
	DefaultRestURL = "https://api.evroc.com"

	// DefaultAuthIssuerURL is the default OIDC issuer URL for authentication.
	DefaultAuthIssuerURL = "https://authn.iam.evroc.com/realms/evroc-customer"

	// DefaultAuthClientID is the default OAuth2 client identifier.
	DefaultAuthClientID = "csi-driver"

	// DefaultMaxVolumesPerNode is the default maximum number of volumes per node.
	// This is a conservative limit based on:
	// - QEMU virtio-scsi controller supports up to 255 targets by default
	// - Practical resource limits (memory, I/O bandwidth)
	// - KubeVirt hotplug disk attachment capabilities
	//
	// This is NOT a hard SCSI limit - virtio-scsi can support more devices.
	// Adjust this value based on your KubeVirt cluster configuration and workload requirements.
	// Set to 0 to indicate unlimited (backend enforces limits if needed).
	DefaultMaxVolumesPerNode = 128

	// DefaultTotalCapacityGB is the default total storage capacity in GB.
	// Used when actual quota cannot be queried from the API.
	DefaultTotalCapacityGB = 1000

	// DefaultDeviceScanTimeout is the default maximum time to wait for a block device to appear.
	DefaultDeviceScanTimeout = 30 * time.Second

	// DefaultDeviceScanInterval is the default polling interval for checking device availability.
	DefaultDeviceScanInterval = 2 * time.Second

	// DefaultAttachmentPollTimeout is the default timeout for waiting for attachment serial population.
	DefaultAttachmentPollTimeout = 2 * time.Minute

	// DefaultDiskResizePollTimeout is the default timeout for waiting for a
	// disk's status to reflect a new size after a resize (Patch) request.
	DefaultDiskResizePollTimeout = 2 * time.Minute

	// DefaultDiskResizePollInterval is the default polling interval for
	// checking a disk's status size after a resize request.
	DefaultDiskResizePollInterval = 5 * time.Second

	// DefaultMetricsCollectionInterval is the default interval for collecting attachment state metrics.
	DefaultMetricsCollectionInterval = 30 * time.Second

	// DefaultRestAPIMaxRetries is the default maximum number of retry attempts for REST API errors.
	DefaultRestAPIMaxRetries = 3

	// DefaultRestAPIRetryInitialDelay is the default initial retry delay for REST API errors.
	DefaultRestAPIRetryInitialDelay = 1 * time.Second

	// DefaultRestAPIRetryMaxDelay is the default maximum retry delay with exponential backoff.
	DefaultRestAPIRetryMaxDelay = 10 * time.Second

	// DefaultRestAPITimeout is the default HTTP client timeout for REST API calls.
	DefaultRestAPITimeout = 30 * time.Second
)

// Config holds the CSI driver configuration loaded from YAML.
type Config struct {
	// DEPRECATED: evroc platform configuration - use `api` and `context` instead
	Evroc EvrocConfig `yaml:"evroc"`

	// API endpoints
	API sdkconfig.APIConfig `yaml:"api"`

	// Project/Organization context
	Context sdkconfig.ContextConfig `yaml:"context"`

	// Authentication configuration.
	Auth sdkconfig.AuthConfig `yaml:"auth"`

	// DEPRECATED: use context instead - Infrastructure configuration.
	Infrastructure InfrastructureConfig `yaml:"infrastructure,omitempty"`

	// CSI driver configuration.
	CSI CSIConfig `yaml:"csi,omitempty"`
}

// EvrocConfig holds evroc platform connection parameters.
type EvrocConfig struct {
	// Deprecated - use api.base_url instead - RestURL is the evroc REST API server URL.
	RestURL string `yaml:"restURL,omitempty"`

	// Deprecatated - use context.organization instead
	// Organization is the evroc organization name.
	Organization string `yaml:"organization"`

	// Deprecated - use context.project instead
	// Project is the workspace where VMs and disks are created.
	Project string `yaml:"project"`
}

// InfrastructureConfig holds infrastructure-related configuration.
type InfrastructureConfig struct {
	// Deprecated: Use config.region instead = Region is the cloud region.
	Region string `yaml:"region,omitempty"`
}

// CSIConfig holds CSI driver-specific configuration.
type CSIConfig struct {
	// Identifier uniquely identifies this CSI driver instance.
	// Used to label Disk objects that belong to this driver.
	Identifier string `yaml:"identifier,omitempty"`

	// MaxVolumesPerNode is the maximum number of volumes that can be attached to a single node.
	// Defaults to 128 if not specified.
	// Any negative value disables limit checking (backend enforces limits if needed).
	MaxVolumesPerNode int64 `yaml:"maxVolumesPerNode,omitempty"`

	// TotalCapacityGB is the total storage capacity quota in GB.
	// Used for reporting available capacity to Kubernetes.
	// Defaults to 1000 GB (1 TB) if not specified or 0.
	TotalCapacityGB int64 `yaml:"totalCapacityGB,omitempty"`

	// DeviceScanTimeout is the maximum time to wait for a block device to appear during volume staging.
	// Defaults to 60 seconds if not specified or 0.
	DeviceScanTimeout time.Duration `yaml:"deviceScanTimeout,omitempty"`

	// DeviceScanInterval is the polling interval for checking device availability during volume staging.
	// Defaults to 2 seconds if not specified or 0.
	DeviceScanInterval time.Duration `yaml:"deviceScanInterval,omitempty"`

	// AttachmentPollTimeout is the timeout for waiting for attachment serial number population.
	// Defaults to 2 minutes if not specified or 0.
	AttachmentPollTimeout time.Duration `yaml:"attachmentPollTimeout,omitempty"`

	// DiskResizePollTimeout is the timeout for waiting for a disk's status
	// to reflect the new size after a resize request.
	// Defaults to 2 minutes if not specified or 0.
	DiskResizePollTimeout time.Duration `yaml:"diskResizePollTimeout,omitempty"`

	// DiskResizePollInterval is the polling interval for checking a disk's
	// status size after a resize request.
	// Defaults to 5 seconds if not specified or 0.
	DiskResizePollInterval time.Duration `yaml:"diskResizePollInterval,omitempty"`

	// MetricsCollectionInterval is the interval for periodic collection of attachment state metrics.
	// Defaults to 30 seconds if not specified or 0.
	MetricsCollectionInterval time.Duration `yaml:"metricsCollectionInterval,omitempty"`

	// RestAPIMaxRetries is the maximum number of retry attempts for transient REST API errors.
	// Defaults to 3 if not specified or 0.
	RestAPIMaxRetries int `yaml:"restAPIMaxRetries,omitempty"`

	// RestAPIRetryInitialDelay is the initial retry delay for REST API errors with exponential backoff.
	// Defaults to 1 second if not specified or 0.
	RestAPIRetryInitialDelay time.Duration `yaml:"restAPIRetryInitialDelay,omitempty"`

	// RestAPIRetryMaxDelay is the maximum retry delay with exponential backoff for REST API errors.
	// Defaults to 10 seconds if not specified or 0.
	RestAPIRetryMaxDelay time.Duration `yaml:"restAPIRetryMaxDelay,omitempty"`

	// RestAPITimeout is the HTTP client timeout for REST API calls.
	// Defaults to 30 seconds if not specified or 0.
	RestAPITimeout time.Duration `yaml:"restAPITimeout,omitempty"`
}

// Load loads configuration from YAML file at /etc/evroc-csi/config.yaml.
func Load() (*Config, error) {
	configFilePath := fmt.Sprintf("%s/%s", ConfigPath, ConfigFileName)
	return LoadFromPath(configFilePath)
}

// LoadFromPath loads configuration from a YAML file at the specified path.
// The configPath parameter should be the full path to the config file.
func LoadFromPath(configPath string) (*Config, error) {
	// Read YAML configuration file.
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// Parse YAML.
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Apply defaults.
	cfg.applyDefaults()

	// Validate configuration.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// NewWithDefaults creates a new Config with only default values applied.
// This is useful for node-only mode where no configuration file is available.
// The returned config will not have auth or evroc settings, only CSI defaults.
func NewWithDefaults() *Config {
	cfg := &Config{}
	cfg.applyDefaults()
	return cfg
}

// applyDefaults applies default values to optional configuration fields.
func (c *Config) applyDefaults() {
	// Apply SDK defaults (derives client_id, token_url, api base_url).
	sdkCfg := &sdkconfig.Config{
		Auth:    c.Auth,
		API:     c.API,
		Context: c.Context,
	}
	sdkCfg.SetDefaults()
	c.Auth = sdkCfg.Auth
	c.API = sdkCfg.API

	// Apply CSI driver defaults.
	// MaxVolumesPerNode: only apply default if not set (0).
	// If set to negative (e.g., -1), leave as-is for unlimited.
	if c.CSI.MaxVolumesPerNode == 0 {
		c.CSI.MaxVolumesPerNode = DefaultMaxVolumesPerNode
	}
	if c.CSI.TotalCapacityGB <= 0 {
		c.CSI.TotalCapacityGB = DefaultTotalCapacityGB
	}
	if c.CSI.DeviceScanTimeout <= 0 {
		c.CSI.DeviceScanTimeout = DefaultDeviceScanTimeout
	}
	if c.CSI.DeviceScanInterval <= 0 {
		c.CSI.DeviceScanInterval = DefaultDeviceScanInterval
	}
	if c.CSI.AttachmentPollTimeout <= 0 {
		c.CSI.AttachmentPollTimeout = DefaultAttachmentPollTimeout
	}
	if c.CSI.DiskResizePollTimeout <= 0 {
		c.CSI.DiskResizePollTimeout = DefaultDiskResizePollTimeout
	}
	if c.CSI.DiskResizePollInterval <= 0 {
		c.CSI.DiskResizePollInterval = DefaultDiskResizePollInterval
	}
	if c.CSI.MetricsCollectionInterval <= 0 {
		c.CSI.MetricsCollectionInterval = DefaultMetricsCollectionInterval
	}
	if c.CSI.RestAPIMaxRetries <= 0 {
		c.CSI.RestAPIMaxRetries = DefaultRestAPIMaxRetries
	}
	if c.CSI.RestAPIRetryInitialDelay <= 0 {
		c.CSI.RestAPIRetryInitialDelay = DefaultRestAPIRetryInitialDelay
	}
	if c.CSI.RestAPIRetryMaxDelay <= 0 {
		c.CSI.RestAPIRetryMaxDelay = DefaultRestAPIRetryMaxDelay
	}
	if c.CSI.RestAPITimeout <= 0 {
		c.CSI.RestAPITimeout = DefaultRestAPITimeout
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	// Validate auth and project configuration
	conf := c.SDKConfig()
	err := conf.Validate()
	if err != nil {
		return err
	}

	// Validate optional CSI identifier if provided
	if c.CSI.Identifier != "" && !isValidIdentifier(c.CSI.Identifier) {
		return fmt.Errorf("csi.identifier must contain only alphanumeric characters, hyphens, and underscores")
	}

	return nil
}

// isValidIdentifier validates that an identifier contains only alphanumeric characters, hyphens, and underscores.
func isValidIdentifier(id string) bool {
	matched, err := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
	if err != nil {
		return false
	}
	return matched
}

// String returns a string representation of the config (with sensitive data redacted).
func (c *Config) String() string {
	return fmt.Sprintf(`Config{
	Auth: {
		TokenURL: %s,
		ClientID: %s,
		Username: %s,
		Password: <redacted - %d chars>,
		Token: <redacted- %d chars>,
		RefreshToken: <redacted- %d chars>,
		Scopes: %v,
		ServiceAccountId %s, 
		ServiceAccountSecret: <redacted - %d chars>
	},
	Evroc: {
		RestURL: %s,
		Organization: %s,
		Project: %s
	},
	Infrastructure: {
		Region: %s
	},
	API: {
		BaseURL: %s,
	},
	Context: {
		Organization: %s,
		Project: %s,
		Region: %s,
	},
	CSI: {
		Identifier: %s
	}
}`,
		c.Auth.TokenURL,
		c.Auth.ClientID,
		c.Auth.Username,      // nolint:staticcheck
		len(c.Auth.Password), // nolint:staticcheck
		len(c.Auth.Token),
		len(c.Auth.RefreshToken),
		c.Auth.Scopes,
		c.Auth.ServiceAccountID,
		len(c.Auth.ServiceAccountSecret),
		c.Evroc.RestURL,
		c.Evroc.Organization,
		c.Evroc.Project,
		c.Infrastructure.Region,
		c.API.BaseURL,
		c.Context.Organization,
		c.Context.Project,
		c.Context.Region,
		c.CSI.Identifier,
	)
}

func (c *Config) SDKConfig() sdkconfig.Config {
	sdkConfig := sdkconfig.Config{
		Auth:    c.Auth,
		API:     c.API,
		Context: c.Context,
	}
	if c.API.BaseURL == "" {
		sdkConfig.API.BaseURL = c.Evroc.RestURL
	}
	if c.Context.Organization == "" {
		sdkConfig.Context.Organization = c.Evroc.Organization
	}
	if c.Context.Project == "" {
		sdkConfig.Context.Project = c.Evroc.Project
	}
	if c.Context.Region == "" {
		sdkConfig.Context.Region = c.Infrastructure.Region
	}
	sdkConfig.SetDefaults()
	return sdkConfig
}
