// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package controller

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/evroc-oss/evroc-csi-driver/test/mocks"
	"github.com/stretchr/testify/require"
	"log/slog"
	"os"
	"time"
)

func TestCreateVolume_CapacityRounding(t *testing.T) {
	mock := mocks.NewMockStorageBackend(slog.New(slog.NewTextHandler(os.Stdout, nil)), nil)
	svc := NewControllerService(slog.New(slog.NewTextHandler(os.Stdout, nil)), mock, 1000, 128, 60*time.Second, 3, 1*time.Second, 30*time.Second, metrics.NewManager())

	resp, err := svc.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
		Name:          "test",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 1*1024*1024*1024 + 1}, // 1GB + 1 byte
		VolumeCapabilities: []*csi.VolumeCapability{{
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		}},
	})

	require.NoError(t, err)
	require.GreaterOrEqual(t, resp.Volume.CapacityBytes, int64(1*1024*1024*1024+1))
}
