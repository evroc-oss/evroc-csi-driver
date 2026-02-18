// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/auth"
	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/evroc-oss/evroc-csi-driver/pkg/driver"
	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc/rest"
	"github.com/evroc-oss/evroc-csi-driver/pkg/filesystem"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/evroc-oss/evroc-csi-driver/pkg/node"
	"github.com/evroc-oss/evroc-csi-driver/pkg/version"
	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	endpoint = flag.String("endpoint", "/tmp/csi.sock", "CSI endpoint (Unix socket path)")
	nodeID   = flag.String("node-id", "", "Node ID (defaults to hostname)")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logJSON  = flag.Bool("log-json", false, "Use JSON log format")
	mode     = flag.String("mode", string(driver.AllMode),
		"The operation mode of the CSI driver (controller, node, or all)")
	configPath  = flag.String("config", "/etc/evroc-csi/config.yaml", "Path to configuration file")
	metricsPort = flag.Int("metrics-port", 9090, "Port for Prometheus metrics endpoint")
	showVersion = flag.Bool("version", false, "Show version information and exit")
)

func main() {
	flag.Parse()

	// Show version and exit if requested
	if *showVersion {
		fmt.Println(version.Get().String())
		os.Exit(0)
	}

	// Configure structured logger
	logger := setupLogger(*logLevel, *logJSON)

	// Set up controller-runtime logger to discard
	// Controller-runtime's internal logs are not useful for our CSI driver
	// and the delegating logger mechanism causes warnings when creating clients
	// in goroutines (e.g., during token refresh)
	ctrllog.SetLogger(logr.Discard())

	// Use hostname as node ID if not provided
	if *nodeID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			logger.Error("Failed to get hostname", "error", err)
			os.Exit(1)
		}
		*nodeID = hostname
	}

	versionInfo := version.Get()
	logger.Info("Starting evroc CSI Driver",
		"version", versionInfo.Version,
		"gitCommit", versionInfo.GitCommit,
		"buildDate", versionInfo.BuildDate,
		"goVersion", versionInfo.GoVersion,
		"endpoint", *endpoint,
		"nodeID", *nodeID,
		"logLevel", *logLevel,
		"mode", *mode,
		"configPath", *configPath,
		"metricsPort", *metricsPort)

	// Create metrics manager and server
	metricsManager := metrics.NewManager()
	metricsServer := metrics.NewServer(metricsManager, *metricsPort)

	// Start metrics server in background
	go func() {
		logger.Info("Starting metrics server", "port", *metricsPort)
		if err := metricsServer.Run(); err != nil {
			logger.Error("Metrics server error", "error", err)
		}
	}()

	// Load configuration
	// Required for controller mode, optional for node mode
	var evrocConfig *config.Config
	switch *mode {
	case "controller", "all":
		// Config is required for controller mode
		var err error
		evrocConfig, err = config.LoadFromPath(*configPath)
		if err != nil {
			logger.Error("Failed to load configuration", "error", err, "path", *configPath)
			os.Exit(1)
		}
		logger.Info("Configuration loaded",
			"restURL", evrocConfig.Evroc.RestURL,
			"org", evrocConfig.Evroc.Organization,
			"project", evrocConfig.Evroc.Project)
	case "node":
		// Config is optional for node mode - try to load it but don't fail if missing
		var err error
		evrocConfig, err = config.LoadFromPath(*configPath)
		if err != nil {
			// Only accept "file not found" errors - use defaults and continue
			// Other errors (permissions, YAML parsing, validation) indicate real problems
			if errors.Is(err, os.ErrNotExist) {
				logger.Info("No configuration file found for node mode, using defaults", "path", *configPath)
				evrocConfig = config.NewWithDefaults()
			} else {
				logger.Error("Failed to load configuration for node mode", "path", *configPath, "error", err)
				os.Exit(1)
			}
		} else {
			logger.Info("Configuration loaded for node mode")
		}
	}

	// Create driver configuration
	driverConfig := &driver.Config{
		Endpoint:    *endpoint,
		NodeID:      *nodeID,
		Logger:      logger,
		Mode:        driver.Mode(*mode),
		EvrocConfig: evrocConfig,
		Metrics:     metricsManager,
	}

	// Create storage backend for controller mode
	if *mode == string(driver.ControllerMode) || *mode == string(driver.AllMode) {
		// Create auth client
		authClient, err := auth.NewClient(context.Background(), evrocConfig, logger)
		if err != nil {
			logger.Error("Failed to create auth client", "error", err)
			os.Exit(1)
		}

		// Create REST API storage backend
		logger.Info("Initializing REST API backend")
		storageBackend, err := rest.NewClient(context.Background(), authClient, evrocConfig, logger, metricsManager)
		if err != nil {
			logger.Error("Failed to create REST client", "error", err)
			os.Exit(1)
		}
		logger.Info("Storage backend initialized")

		driverConfig.StorageBackend = storageBackend
	}

	// Create filesystem operations and Kubernetes client for node mode
	if *mode == string(driver.NodeMode) || *mode == string(driver.AllMode) {
		// Create Kubernetes client for zone detection
		kubeConfig, err := restclient.InClusterConfig()
		if err != nil {
			logger.Error("Failed to get in-cluster config", "error", err)
			os.Exit(1)
		}

		kubeClient, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			logger.Error("Failed to create Kubernetes client", "error", err)
			os.Exit(1)
		}
		driverConfig.KubeClient = kubeClient

		// Validate node has zone label at startup (fail fast)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		zone, err := node.GetNodeZoneFromKubernetes(ctx, kubeClient, *nodeID)
		if err != nil {
			logger.Error("Node zone validation failed", "error", err)
			os.Exit(1)
		}
		logger.Info("Node zone validated at startup", "nodeID", *nodeID, "zone", zone)

		// Use config values (always non-nil with defaults applied)
		driverConfig.FilesystemOps = filesystem.NewUnixOperations(logger, evrocConfig.CSI.DeviceScanInterval)
		logger.Info("Filesystem operations initialized (Unix)", "deviceScanInterval", evrocConfig.CSI.DeviceScanInterval)
	}

	// Create and run the driver
	drv, err := driver.NewDriver(driverConfig)
	if err != nil {
		logger.Error("Failed to create driver", "error", err)
		os.Exit(1)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- drv.Run()
	}()

	// Wait for shutdown signal or driver error
	select {
	case <-sigChan:
		logger.Info("Received shutdown signal, shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := metricsServer.Shutdown(ctx); err != nil {
			logger.Error("Error shutting down metrics server", "error", err)
		}
	case err := <-errChan:
		if err != nil {
			logger.Error("Driver error", "error", err)
			os.Exit(1)
		}
	}
}

// setupLogger creates a configured slog.Logger.
func setupLogger(level string, useJSON bool) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}

	var handler slog.Handler
	if useJSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
