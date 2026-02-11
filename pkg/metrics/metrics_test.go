// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	manager := NewManager()
	require.NotNil(t, manager, "expected non-nil manager")
	require.NotNil(t, manager.Registry(), "expected non-nil registry")
}

func TestNewServer(t *testing.T) {
	manager := NewManager()
	server := NewServer(manager, 0)

	require.NotNil(t, server, "expected non-nil server")
	require.NotNil(t, server.server, "expected non-nil HTTP server")

	// Verify default port is used
	expectedAddr := ":9090"
	require.Equal(t, expectedAddr, server.server.Addr, "expected default port")
}

func TestNewServerCustomPort(t *testing.T) {
	manager := NewManager()
	customPort := 8080
	server := NewServer(manager, customPort)

	expectedAddr := ":8080"
	require.Equal(t, expectedAddr, server.server.Addr, "expected custom port")
}

func TestAuthMetrics(t *testing.T) {
	manager := NewManager()

	// Test successful authentication
	manager.RecordAuthOperation("token_refresh", 0.5)

	// Test failed authentication with different error types
	manager.RecordAuthOperationError("token_refresh", 1.2, "timeout")
	manager.RecordAuthOperationError("initial_auth", 0.8, "unauthorized")

	// Verify metrics are not nil (actual values would require more complex testing)
	require.NotNil(t, manager.AuthOperationsTotal, "expected non-nil auth operations total")
	require.NotNil(t, manager.AuthOperationsDuration, "expected non-nil auth operations duration")
	require.NotNil(t, manager.AuthOperationsErrors, "expected non-nil auth operations errors")
}
