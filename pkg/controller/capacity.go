// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package controller

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// capacityFromRange derives the requested capacity in bytes (and the
// corresponding size in MiB, rounded up) from a CSI CapacityRange.
//
// It is shared by CreateVolume and ControllerExpandVolume, both of which
// interpret a CapacityRange the same way:
//   - RequiredBytes takes precedence; if it is 0, LimitBytes is used (the CSI
//     spec allows the CO to specify only a limit).
//   - The byte value is rounded UP to the nearest MiB so the disk is at least
//     as large as requested.
//
// Returns an InvalidArgument gRPC error if no capacity can be determined.
func capacityFromRange(capacityRange *csi.CapacityRange) (capacityBytes int64, capacityMB int32, err error) {
	if capacityRange != nil {
		capacityBytes = capacityRange.GetRequiredBytes()
		if capacityBytes == 0 {
			capacityBytes = capacityRange.GetLimitBytes()
		}
	}

	if capacityBytes == 0 {
		return 0, 0, status.Error(codes.InvalidArgument,
			"capacity range must specify required_bytes or limit_bytes")
	}

	capacityMB = int32((capacityBytes + 1024*1024 - 1) / (1024 * 1024))

	return capacityBytes, capacityMB, nil
}

// capacityMBToBytes converts a size in MiB back to bytes.
func capacityMBToBytes(capacityMB int32) int64 {
	return int64(capacityMB) * 1024 * 1024
}
