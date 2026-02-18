// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// DefaultMetricsPort is the default port for metrics endpoint.
	DefaultMetricsPort = 9090

	// DefaultMetricsPath is the default path for metrics endpoint.
	DefaultMetricsPath = "/metrics"
)

// Server provides HTTP endpoints for Prometheus metrics.
type Server struct {
	server  *http.Server
	manager *Manager
}

// NewServer creates a new metrics HTTP server.
func NewServer(manager *Manager, port int) *Server {
	if port == 0 {
		port = DefaultMetricsPort
	}

	mux := http.NewServeMux()

	// Metrics endpoint
	mux.Handle(DefaultMetricsPath, promhttp.HandlerFor(
		manager.Registry(),
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	return &Server{
		server:  server,
		manager: manager,
	}
}

// Run starts the metrics HTTP server and blocks until it stops.
func (s *Server) Run() error {
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the metrics server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
