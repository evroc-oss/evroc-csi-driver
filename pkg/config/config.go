// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigPath is the path where configuration files are mounted.
	ConfigPath = "/etc/evroc-csi"

	// ConfigFileName is the name of the YAML configuration file.
	ConfigFileName = "config.yaml"

	// DefaultRestURL is the default production REST API server URL.
	DefaultRestURL = "https://api.cloud.evroc.com"

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
	// evroc platform configuration.
	Evroc EvrocConfig `yaml:"evroc"`

	// Authentication configuration.
	Auth AuthConfig `yaml:"auth"`

	// Infrastructure configuration.
	Infrastructure InfrastructureConfig `yaml:"infrastructure,omitempty"`

	// CSI driver configuration.
	CSI CSIConfig `yaml:"csi,omitempty"`
}

// EvrocConfig holds evroc platform connection parameters.
type EvrocConfig struct {
	// RestURL is the evroc REST API server URL.
	RestURL string `yaml:"restURL,omitempty"`

	// Organization is the evroc organization name.
	Organization string `yaml:"organization"`

	// Project is the workspace where VMs and disks are created.
	Project string `yaml:"project"`

	// InsecureTLS skips TLS certificate verification (for development/testing only).
	// WARNING: This is insecure and should never be used in production.
	InsecureTLS bool `yaml:"insecureTLS,omitempty"`
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	// IssuerURL is the OIDC issuer URL for authentication.
	IssuerURL string `yaml:"issuerURL"`

	// ClientID is the OAuth2 client identifier.
	// Defaults to "evroc-cluster-client" if not specified.
	ClientID string `yaml:"clientID,omitempty"`

	// Username for authentication.
	Username string `yaml:"username"`

	// Password for authentication.
	Password string `yaml:"password"`
}

// InfrastructureConfig holds infrastructure-related configuration.
type InfrastructureConfig struct {
	// Region is the cloud region.
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
	// Apply evroc platform defaults.
	if c.Evroc.RestURL == "" {
		c.Evroc.RestURL = DefaultRestURL
	}

	// Apply auth defaults.
	if c.Auth.IssuerURL == "" {
		c.Auth.IssuerURL = DefaultAuthIssuerURL
	}
	if c.Auth.ClientID == "" {
		c.Auth.ClientID = DefaultAuthClientID
	}

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
	// Validate auth configuration
	if c.Auth.IssuerURL == "" {
		return fmt.Errorf("auth.issuerURL is required")
	}
	if err := validateURL(c.Auth.IssuerURL, "auth.issuerURL"); err != nil {
		return err
	}

	if c.Auth.ClientID == "" {
		return fmt.Errorf("auth.clientID is required")
	}

	if c.Auth.Username == "" {
		return fmt.Errorf("auth.username is required")
	}
	if c.Auth.Password == "" {
		return fmt.Errorf("auth.password is required")
	}

	// Validate evroc platform configuration
	if c.Evroc.RestURL == "" {
		return fmt.Errorf("evroc.restURL is required")
	}
	if err := validateURL(c.Evroc.RestURL, "evroc.restURL"); err != nil {
		return err
	}

	if c.Evroc.Organization == "" {
		return fmt.Errorf("evroc.organization is required")
	}
	// Organization should be a valid identifier (alphanumeric, hyphens, underscores)
	if !isValidIdentifier(c.Evroc.Organization) {
		return fmt.Errorf("evroc.organization must contain only alphanumeric characters, hyphens, and underscores")
	}

	if c.Evroc.Project == "" {
		return fmt.Errorf("evroc.project is required")
	}
	// Project should be a valid identifier (alphanumeric, hyphens, underscores)
	if !isValidIdentifier(c.Evroc.Project) {
		return fmt.Errorf("evroc.project must contain only alphanumeric characters, hyphens, and underscores")
	}

	// Validate optional CSI identifier if provided
	if c.CSI.Identifier != "" && !isValidIdentifier(c.CSI.Identifier) {
		return fmt.Errorf("csi.identifier must contain only alphanumeric characters, hyphens, and underscores")
	}

	return nil
}

// validateURL validates that a string is a valid HTTP or HTTPS URL.
func validateURL(urlStr, fieldName string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", fieldName, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must be an HTTP or HTTPS URL", fieldName)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", fieldName)
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
	return fmt.Sprintf(
		"Config{Auth: {IssuerURL: %s, ClientID: %s, Username: %s, Password: <redacted>}, "+
			"Evroc: {RestURL: %s, Organization: %s, Project: %s}, "+
			"Infrastructure: {Region: %s}, CSI: {Identifier: %s}}",
		c.Auth.IssuerURL, c.Auth.ClientID, c.Auth.Username,
		c.Evroc.RestURL, c.Evroc.Organization, c.Evroc.Project,
		c.Infrastructure.Region, c.CSI.Identifier,
	)
}
