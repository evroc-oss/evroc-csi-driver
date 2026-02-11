// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


// Package rest provides a client for the evroc REST API for Disk and HotswapDiskAttachment operations.
package rest

// Error represents a standard API error response.
type Error struct {
	// Reason is a stable, textual error code for programmatic handling.
	Reason string `json:"reason"`

	// Debug provides additional human-readable diagnostic information.
	Debug string `json:"debug"`
}
