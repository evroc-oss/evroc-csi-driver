// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package driver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/auth"
	driverconfig "github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/evroc-oss/evroc-csi-driver/pkg/controller"
	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc"
	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc/rest"
	"github.com/evroc-oss/evroc-csi-driver/pkg/filesystem"
	"github.com/evroc-oss/evroc-csi-driver/pkg/identity"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/evroc-oss/evroc-csi-driver/pkg/node"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
)

// Mode represents the CSI driver operation mode.
type Mode string

const (
	// ControllerMode runs only the controller service.
	ControllerMode Mode = "controller"
	// NodeMode runs only the node service.
	NodeMode Mode = "node"
	// AllMode runs both controller and node services.
	AllMode Mode = "all"
)

// Validate returns an error if the mode is invalid.
func (m Mode) Validate() error {
	switch m {
	case ControllerMode, NodeMode, AllMode:
		return nil
	default:
		return fmt.Errorf("invalid mode %q: must be %q, %q, or %q", m, ControllerMode, NodeMode, AllMode)
	}
}

// Config holds the driver configuration.
type Config struct {
	Logger *slog.Logger

	// Configuration for creating dependencies and CSI parameters
	EvrocConfig *driverconfig.Config

	// Dependencies for controller service (optional - if nil, created from EvrocConfig)
	StorageBackend evroc.StorageBackend

	// Dependencies for node service
	FilesystemOps filesystem.Operations // Required for node mode
	KubeClient    kubernetes.Interface  // Required for node mode (zone detection)

	// Metrics manager for Prometheus metrics collection
	Metrics *metrics.Manager

	Endpoint       string
	NodeID         string
	DeviceByIDPath string // Optional: override default /dev/disk/by-id path
	Mode           Mode
}

// Driver represents the CSI driver.
type Driver struct {
	config     *Config
	logger     *slog.Logger
	grpcServer *grpc.Server
	listener   net.Listener

	identityService   *identity.Service
	controllerService *controller.Service
	nodeService       *node.Service
}

// NewDriver creates a new CSI driver.
func NewDriver(config *Config) (*Driver, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if config.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}
	if config.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	logger := config.Logger

	// Default to AllMode if not specified
	if config.Mode == "" {
		config.Mode = AllMode
	}

	// Validate mode
	if err := config.Mode.Validate(); err != nil {
		return nil, err
	}

	// Ensure EvrocConfig is always non-nil with defaults
	// This eliminates nil checks throughout the codebase
	if config.EvrocConfig == nil {
		config.EvrocConfig = driverconfig.NewWithDefaults()
	}

	d := &Driver{
		config:          config,
		logger:          logger,
		identityService: identity.NewIdentityService(logger),
	}

	// Create controller service if needed
	if config.Mode == ControllerMode || config.Mode == AllMode {
		var storageBackend evroc.StorageBackend

		// Use injected storage backend if provided, otherwise create from EvrocConfig
		if config.StorageBackend != nil {
			storageBackend = config.StorageBackend
			logger.Info("Using injected storage backend")
		} else {
			if config.EvrocConfig == nil {
				return nil, fmt.Errorf("either StorageBackend or EvrocConfig is required for controller mode")
			}

			// Create auth client
			authClient, err := auth.NewClient(context.Background(), config.EvrocConfig, logger)
			if err != nil {
				return nil, fmt.Errorf("failed to create auth client: %w", err)
			}

			// Create REST API client
			restClient, err := rest.NewClient(context.Background(), authClient, config.EvrocConfig, logger, config.Metrics)
			if err != nil {
				return nil, fmt.Errorf("failed to create REST API client: %w", err)
			}

			logger.Info("REST API client initialized",
				"restURL", config.EvrocConfig.Evroc.RestURL,
				"org", config.EvrocConfig.Evroc.Organization,
				"project", config.EvrocConfig.Evroc.Project)

			storageBackend = restClient
		}

		if config.EvrocConfig == nil {
			return nil, fmt.Errorf("EvrocConfig is required for controller mode")
		}

		d.controllerService = controller.NewControllerService(
			logger,
			storageBackend,
			config.EvrocConfig.CSI.TotalCapacityGB,
			config.EvrocConfig.CSI.MaxVolumesPerNode,
			config.EvrocConfig.CSI.MetricsCollectionInterval,
			config.EvrocConfig.CSI.RestAPIMaxRetries,
			config.EvrocConfig.CSI.RestAPIRetryInitialDelay,
			config.EvrocConfig.CSI.RestAPIRetryMaxDelay,
			config.Metrics,
		)
	}

	// Create node service if needed
	if config.Mode == NodeMode || config.Mode == AllMode {
		var fsOps filesystem.Operations

		// Use injected filesystem operations if provided, otherwise create default
		if config.FilesystemOps != nil {
			fsOps = config.FilesystemOps
			logger.Info("Using injected filesystem operations")
		} else {
			// Create filesystem operations with config (guaranteed non-nil)
			fsOps = filesystem.NewUnixOperations(logger, config.EvrocConfig.CSI.DeviceScanInterval)
		}

		// MaxVolumesPerNode defaults to node.MaxVolumesPerNode if not configured
		maxVolumesPerNode := int64(node.MaxVolumesPerNode)
		if config.EvrocConfig.CSI.MaxVolumesPerNode > 0 {
			maxVolumesPerNode = config.EvrocConfig.CSI.MaxVolumesPerNode
		}

		// Device scan timeout and interval from config (guaranteed non-nil)
		deviceScanTimeout := config.EvrocConfig.CSI.DeviceScanTimeout
		deviceScanInterval := config.EvrocConfig.CSI.DeviceScanInterval

		logger.Info("Node service configuration",
			"maxVolumesPerNode", maxVolumesPerNode,
			"deviceScanTimeout", deviceScanTimeout,
			"deviceScanInterval", deviceScanInterval)

		// Kubernetes client is required for node zone detection
		if config.KubeClient == nil {
			return nil, fmt.Errorf("KubeClient is required for node mode")
		}

		d.nodeService = node.NewNodeService(config.NodeID, logger, fsOps, maxVolumesPerNode, deviceScanTimeout, deviceScanInterval, config.Metrics, config.KubeClient)

		// Override device path if provided
		if config.DeviceByIDPath != "" {
			d.nodeService.SetDeviceByIDPath(config.DeviceByIDPath)
			logger.Info("Using custom device path", "path", config.DeviceByIDPath)
		}
	}

	return d, nil
}

// Run starts the CSI driver.
func (d *Driver) Run() error {
	// Parse endpoint to extract socket path (strip unix:// prefix if present)
	socketPath := strings.TrimPrefix(d.config.Endpoint, "unix://")

	// Create parent directory if it doesn't exist
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return fmt.Errorf("failed to create socket directory %s: %w", socketDir, err)
	}

	// Remove existing socket if it exists
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix domain socket listener with context
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}
	d.listener = listener

	d.logger.Info("Driver listening", "socketPath", socketPath)

	// Create gRPC server
	d.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(d.loggingInterceptor),
	)

	// Register CSI services based on mode
	var registeredServices []string

	// Identity service is always registered
	csi.RegisterIdentityServer(d.grpcServer, d.identityService)
	registeredServices = append(registeredServices, "Identity")

	// Register controller service if running in controller or all mode
	if d.controllerService != nil {
		csi.RegisterControllerServer(d.grpcServer, d.controllerService)
		registeredServices = append(registeredServices, "Controller")
	}

	// Register node service if running in node or all mode
	if d.nodeService != nil {
		csi.RegisterNodeServer(d.grpcServer, d.nodeService)
		registeredServices = append(registeredServices, "Node")
	}

	d.logger.Info("CSI driver running", "mode", d.config.Mode, "services", registeredServices)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		d.logger.Info("Received shutdown signal, stopping driver")
		d.Stop()
	}()

	// Start serving
	d.logger.Info("Starting gRPC server")
	if err := d.grpcServer.Serve(listener); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// Stop stops the CSI driver.
func (d *Driver) Stop() {
	if d.grpcServer != nil {
		d.grpcServer.GracefulStop()
		d.logger.Info("gRPC server stopped")
	}

	if d.listener != nil {
		if err := d.listener.Close(); err != nil {
			d.logger.Warn("Failed to close listener", "error", err)
		} else {
			d.logger.Info("Listener closed")
		}
	}

	// Clean up socket file
	socketPath := strings.TrimPrefix(d.config.Endpoint, "unix://")
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		d.logger.Warn("Failed to remove socket file", "error", err)
	}
}

// loggingInterceptor logs all gRPC requests.
func (d *Driver) loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	d.logger.Debug("gRPC request received", "method", info.FullMethod, "request", req)
	resp, err := handler(ctx, req)
	if err != nil {
		d.logger.Debug("gRPC request handled", "method", info.FullMethod, "response", resp, "error", err)
	} else {
		d.logger.Debug("gRPC request handled", "method", info.FullMethod, "response", resp)
	}
	return resp, err
}
