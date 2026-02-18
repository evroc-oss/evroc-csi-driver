// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const (
	namespace = "evroc_csi"
)

// Manager handles Prometheus metrics collection.
type Manager struct {
	registry *prometheus.Registry

	// Volume operation metrics
	VolumeOperationsTotal    *prometheus.CounterVec
	VolumeOperationsDuration *prometheus.HistogramVec
	VolumeOperationsErrors   *prometheus.CounterVec

	// Volume state metrics
	VolumesTotal    *prometheus.GaugeVec
	VolumeSizeBytes *prometheus.GaugeVec

	// Node operation metrics
	NodeOperationsTotal    *prometheus.CounterVec
	NodeOperationsDuration *prometheus.HistogramVec
	NodeOperationsErrors   *prometheus.CounterVec

	// Attachment metrics
	AttachmentsTotal *prometheus.GaugeVec

	// API call metrics
	APICallsTotal    *prometheus.CounterVec
	APICallsDuration *prometheus.HistogramVec
	APICallsErrors   *prometheus.CounterVec

	// Authentication metrics
	AuthOperationsTotal    *prometheus.CounterVec
	AuthOperationsDuration *prometheus.HistogramVec
	AuthOperationsErrors   *prometheus.CounterVec
}

// initializeMetrics creates a Manager with all metric collectors initialized.
// This is the common initialization logic shared by NewManager and NewNoOpManager.
func initializeMetrics(registry *prometheus.Registry) *Manager {
	return &Manager{
		registry: registry,

		// Volume operations counter
		VolumeOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "volume_operations_total",
				Help:      "Total number of volume operations by type and status",
			},
			[]string{"operation", "status"},
		),

		// Volume operations duration histogram
		VolumeOperationsDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "volume_operations_duration_seconds",
				Help:      "Duration of volume operations in seconds",
				Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
			},
			[]string{"operation"},
		),

		// Volume operations errors
		VolumeOperationsErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "volume_operations_errors_total",
				Help:      "Total number of volume operation errors by type",
			},
			[]string{"operation", "error_type"},
		),

		// Current volumes gauge
		VolumesTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "volumes_total",
				Help:      "Total number of volumes by state",
			},
			[]string{"state"},
		),

		// Volume size in bytes
		VolumeSizeBytes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "volume_size_bytes",
				Help:      "Size of volumes in bytes",
			},
			[]string{"volume_id"},
		),

		// Node operations counter
		NodeOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "node_operations_total",
				Help:      "Total number of node operations by type and status",
			},
			[]string{"operation", "status"},
		),

		// Node operations duration
		NodeOperationsDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "node_operations_duration_seconds",
				Help:      "Duration of node operations in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
			},
			[]string{"operation"},
		),

		// Node operations errors
		NodeOperationsErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "node_operations_errors_total",
				Help:      "Total number of node operation errors by type",
			},
			[]string{"operation", "error_type"},
		),

		// Current attachments gauge
		AttachmentsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "attachments_total",
				Help:      "Total number of volume attachments by state",
			},
			[]string{"state"},
		),

		// API calls counter
		APICallsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "api_calls_total",
				Help:      "Total number of API calls by method and status",
			},
			[]string{"method", "status"},
		),

		// API calls duration
		APICallsDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "api_calls_duration_seconds",
				Help:      "Duration of API calls in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
			},
			[]string{"method"},
		),

		// API calls errors
		APICallsErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "api_calls_errors_total",
				Help:      "Total number of API call errors by method and type",
			},
			[]string{"method", "error_type"},
		),

		// Authentication operations counter
		AuthOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "auth_operations_total",
				Help:      "Total number of authentication operations by operation type and status",
			},
			[]string{"operation", "status"},
		),

		// Authentication operations duration
		AuthOperationsDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "auth_operations_duration_seconds",
				Help:      "Duration of authentication operations in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
			},
			[]string{"operation"},
		),

		// Authentication operations errors
		AuthOperationsErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "auth_operations_errors_total",
				Help:      "Total number of authentication operation errors by operation and error type",
			},
			[]string{"operation", "error_type"},
		),
	}
}

// NewManager creates a new metrics manager with all metrics registered and collectors enabled.
func NewManager() *Manager {
	registry := prometheus.NewRegistry()

	// Register default Go metrics (memory, goroutines, etc.)
	registry.MustRegister(collectors.NewGoCollector())

	// Register default process metrics (CPU, file descriptors, etc.)
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := initializeMetrics(registry)

	// Register all metrics
	registry.MustRegister(
		m.VolumeOperationsTotal,
		m.VolumeOperationsDuration,
		m.VolumeOperationsErrors,
		m.VolumesTotal,
		m.VolumeSizeBytes,
		m.NodeOperationsTotal,
		m.NodeOperationsDuration,
		m.NodeOperationsErrors,
		m.AttachmentsTotal,
		m.APICallsTotal,
		m.APICallsDuration,
		m.APICallsErrors,
		m.AuthOperationsTotal,
		m.AuthOperationsDuration,
		m.AuthOperationsErrors,
	)

	return m
}

// NewNoOpManager creates a no-op metrics manager for testing.
// All metrics are initialized but not registered, so writes are discarded.
func NewNoOpManager() *Manager {
	registry := prometheus.NewRegistry()
	return initializeMetrics(registry)
}

// Registry returns the Prometheus registry.
func (m *Manager) Registry() *prometheus.Registry {
	return m.registry
}

// RecordVolumeOperation records a successful volume operation with its duration.
func (m *Manager) RecordVolumeOperation(operation string, duration float64) {
	m.VolumeOperationsDuration.WithLabelValues(operation).Observe(duration)
	m.VolumeOperationsTotal.WithLabelValues(operation, "success").Inc()
}

// RecordVolumeOperationError records a failed volume operation with its duration and error type.
func (m *Manager) RecordVolumeOperationError(operation string, duration float64, errorType string) {
	m.VolumeOperationsDuration.WithLabelValues(operation).Observe(duration)
	m.VolumeOperationsTotal.WithLabelValues(operation, "failure").Inc()
	m.VolumeOperationsErrors.WithLabelValues(operation, errorType).Inc()
}

// RecordNodeOperation records a successful node operation with its duration.
func (m *Manager) RecordNodeOperation(operation string, duration float64) {
	m.NodeOperationsDuration.WithLabelValues(operation).Observe(duration)
	m.NodeOperationsTotal.WithLabelValues(operation, "success").Inc()
}

// RecordNodeOperationError records a failed node operation with its duration and error type.
func (m *Manager) RecordNodeOperationError(operation string, duration float64, errorType string) {
	m.NodeOperationsDuration.WithLabelValues(operation).Observe(duration)
	m.NodeOperationsTotal.WithLabelValues(operation, "failure").Inc()
	m.NodeOperationsErrors.WithLabelValues(operation, errorType).Inc()
}

// RecordAPICall records a successful API call with its duration.
func (m *Manager) RecordAPICall(method string, duration float64) {
	m.APICallsDuration.WithLabelValues(method).Observe(duration)
	m.APICallsTotal.WithLabelValues(method, "success").Inc()
}

// RecordAPICallError records a failed API call with its duration and error type.
func (m *Manager) RecordAPICallError(method string, duration float64, errorType string) {
	m.APICallsDuration.WithLabelValues(method).Observe(duration)
	m.APICallsTotal.WithLabelValues(method, "failure").Inc()
	m.APICallsErrors.WithLabelValues(method, errorType).Inc()
}

// UpdateAttachmentState updates the attachment gauge for a given state.
func (m *Manager) UpdateAttachmentState(state string, delta float64) {
	m.AttachmentsTotal.WithLabelValues(state).Add(delta)
}

// UpdateVolumeState updates the volume gauge for a given state.
func (m *Manager) UpdateVolumeState(state string, delta float64) {
	m.VolumesTotal.WithLabelValues(state).Add(delta)
}

// SetVolumeSize sets the size of a specific volume.
func (m *Manager) SetVolumeSize(volumeID string, sizeBytes float64) {
	m.VolumeSizeBytes.WithLabelValues(volumeID).Set(sizeBytes)
}

// DeleteVolumeSize removes the size metric for a specific volume.
func (m *Manager) DeleteVolumeSize(volumeID string) {
	m.VolumeSizeBytes.DeleteLabelValues(volumeID)
}

// RecordAuthOperation records a successful authentication operation with its duration.
func (m *Manager) RecordAuthOperation(operation string, duration float64) {
	m.AuthOperationsDuration.WithLabelValues(operation).Observe(duration)
	m.AuthOperationsTotal.WithLabelValues(operation, "success").Inc()
}

// RecordAuthOperationError records a failed authentication operation with its duration and error type.
func (m *Manager) RecordAuthOperationError(operation string, duration float64, errorType string) {
	m.AuthOperationsDuration.WithLabelValues(operation).Observe(duration)
	m.AuthOperationsTotal.WithLabelValues(operation, "failure").Inc()
	m.AuthOperationsErrors.WithLabelValues(operation, errorType).Inc()
}
