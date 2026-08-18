// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package controller

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/evroc-oss/evroc-csi-driver/test/mocks"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDeleteVolumeRejectsFQIDAttachment(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	deviceMgr := mocks.NewMockDeviceManager()
	require.NoError(t, deviceMgr.Setup())
	storage := mocks.NewMockStorageBackend(logger, deviceMgr)
	service := NewControllerService(logger, storage, 1000, 128, time.Minute, 3, time.Second, 30*time.Second, metrics.NewManager())

	const volumeID = "csi-pvc-still-attached"
	const nodeID = "worker-2"
	ctx := context.Background()
	_, err := storage.EnsureDiskCreated(ctx, volumeID, 1024, "standard", "se-sto-1")
	require.NoError(t, err)
	storage.AddNode(nodeID)
	require.NoError(t, storage.EnsureAttachmentCreated(ctx, volumeID, nodeID))

	_, err = service.DeleteVolume(ctx, &csi.DeleteVolumeRequest{VolumeId: volumeID})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}
