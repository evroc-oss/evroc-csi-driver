// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package common

import (
	"path/filepath"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ValidateRequiredField validates that a string field is not empty.
func ValidateRequiredField(value, fieldName string) error {
	if value == "" {
		return status.Errorf(codes.InvalidArgument, "%s is required", fieldName)
	}
	return nil
}

// ValidateRequiredFields validates multiple required fields at once.
// Returns the first validation error encountered.
func ValidateRequiredFields(fields map[string]string) error {
	for fieldName, value := range fields {
		if err := ValidateRequiredField(value, fieldName); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePath validates that a path is safe from path traversal attacks.
// It protects against malicious path manipulation by:
// 1. Checking for empty paths
// 2. Detecting path traversal sequences (..)
// 3. Ensuring the path is absolute (starts with /)
//
// Parameters:
//   - path: The file path to validate
//   - fieldName: Name of the field for error messages
//
// Returns an InvalidArgument error if validation fails.
func ValidatePath(path, fieldName string) error {
	if path == "" {
		return status.Errorf(codes.InvalidArgument, "%s is required", fieldName)
	}

	// Check for path traversal attempts
	// Reject any path containing .. to prevent directory traversal
	if strings.Contains(path, "..") {
		return status.Errorf(codes.InvalidArgument, "%s contains invalid path traversal sequence", fieldName)
	}

	// Ensure path is absolute (starts with /)
	// CSI paths from kubelet are always absolute
	if !filepath.IsAbs(path) {
		return status.Errorf(codes.InvalidArgument, "%s must be an absolute path", fieldName)
	}

	return nil
}
