// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package rest

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

// mockAuthClient is a mock implementation for testing
type mockAuthClient struct {
	err   error
	token string
}

func (m *mockAuthClient) GetAccessToken(ctx context.Context) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.token, nil
}

func (m *mockAuthClient) IsTokenValid() bool {
	return m.token != ""
}

func TestNewClient(t *testing.T) {
	cfg := &config.Config{
		Evroc: config.EvrocConfig{
			RestURL:      "https://api.example.com",
			Organization: "test-org",
			Project:      "test-project",
		},
		Infrastructure: config.InfrastructureConfig{
			Region: "se-sto",
		},
	}

	authClient := &mockAuthClient{token: "test-token"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metricsManager := metrics.NewNoOpManager()

	client, err := NewClient(context.Background(), authClient, cfg, logger, metricsManager)

	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "https://api.example.com", client.baseURL.String())
	assert.Equal(t, "test-project", client.projectID)
	assert.Equal(t, "se-sto", client.region)
	assert.Equal(t, "test-token", client.currentToken)
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "403 error",
			err:      assert.AnError,
			expected: false,
		},
		{
			name:     "404 in error message",
			err:      &mockError{msg: "API error 404: NotFound - Resource not found"},
			expected: true,
		},
		{
			name:     "403 in error message",
			err:      &mockError{msg: "API error 403: Forbidden - No access"},
			expected: true,
		},
		{
			name:     "other error",
			err:      &mockError{msg: "API error 500: InternalError - Server error"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNotFound(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAlreadyExists(t *testing.T) {
	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "409 in error message",
			err:      &mockError{msg: "API error 409: Conflict - Resource already exists"},
			expected: true,
		},
		{
			name:     "other error",
			err:      &mockError{msg: "API error 500: InternalError - Server error"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAlreadyExists(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockError is a simple error implementation for testing
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

func TestBuildResourcePath(t *testing.T) {
	client := &Client{
		projectID: "my-project",
		region:    "se-sto",
	}

	tests := []struct {
		name         string
		apiGroup     string
		version      string
		resource     string
		expectedPath string
	}{
		{
			name:         "compute disks",
			apiGroup:     "compute",
			version:      "v1alpha2",
			resource:     "disks",
			expectedPath: "/compute/v1alpha2/projects/my-project/regions/se-sto/disks",
		},
		{
			name:         "compute hotswap disk attachments",
			apiGroup:     "compute",
			version:      "v1alpha2",
			resource:     "hotswapDiskAttachments",
			expectedPath: "/compute/v1alpha2/projects/my-project/regions/se-sto/hotswapDiskAttachments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := client.buildResourcePath(tt.apiGroup, tt.version, tt.resource)
			assert.Equal(t, tt.expectedPath, path)
		})
	}
}

func TestBuildResourceItemPath(t *testing.T) {
	client := &Client{
		projectID: "my-project",
		region:    "se-sto",
	}

	tests := []struct {
		name         string
		apiGroup     string
		version      string
		resource     string
		itemID       string
		expectedPath string
	}{
		{
			name:         "specific disk",
			apiGroup:     "compute",
			version:      "v1alpha2",
			resource:     "disks",
			itemID:       "my-disk-123",
			expectedPath: "/compute/v1alpha2/projects/my-project/regions/se-sto/disks/my-disk-123",
		},
		{
			name:         "specific hotswap disk attachment",
			apiGroup:     "compute",
			version:      "v1alpha2",
			resource:     "hotswapDiskAttachments",
			itemID:       "my-disk-123-vm-456",
			expectedPath: "/compute/v1alpha2/projects/my-project/regions/se-sto/hotswapDiskAttachments/my-disk-123-vm-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := client.buildResourceItemPath(tt.apiGroup, tt.version, tt.resource, tt.itemID)
			assert.Equal(t, tt.expectedPath, path)
		})
	}
}
