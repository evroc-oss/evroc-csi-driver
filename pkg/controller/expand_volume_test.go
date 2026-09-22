// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/evroc-oss/evroc-csi-driver/test/mocks"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// resizeErrorBackend wraps MockStorageBackend and injects an error (if non-nil)
// from EnsureDiskResized, without modifying the shared mock.
type resizeErrorBackend struct {
	*mocks.MockStorageBackend
	resizeErr error
}

func (f *resizeErrorBackend) EnsureDiskResized(ctx context.Context, name string, sizeMB int32) error {
	if f.resizeErr != nil {
		return f.resizeErr
	}
	return f.MockStorageBackend.EnsureDiskResized(ctx, name, sizeMB)
}

func newExpandTestService(t *testing.T, storage evroc.StorageBackend) *Service {
	t.Helper()
	return NewControllerService(
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
		storage,
		1000, 128, 60*time.Second, 3, 1*time.Second, 30*time.Second,
		metrics.NewManager(),
	)
}

// A successful resize returns the requested capacity and always requests
// node expansion.
func TestControllerExpandVolume_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	storage := mocks.NewMockStorageBackend(logger, nil)
	svc := newExpandTestService(t, storage)

	const volumeID = "csi-pvc-resize-test"
	_, err := storage.EnsureDiskCreated(context.Background(), volumeID, 1024, "standard", "se-sto-1")
	require.NoError(t, err)

	const reqBytes = 2 * 1024 * 1024 * 1024 // 2 GiB
	resp, err := svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId:      volumeID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: reqBytes},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, int64(reqBytes), resp.CapacityBytes)
	require.True(t, resp.NodeExpansionRequired)

	// The mock should have recorded the rounded-up MB size.
	disk, err := storage.GetDisk(context.Background(), volumeID)
	require.NoError(t, err)
	require.Equal(t, int32(2048), disk.Spec.DiskSize.Amount)
}

// Non-MiB-aligned requests are rounded up to the nearest MiB for the disk
// resize, but the response carries the raw requested bytes.
func TestControllerExpandVolume_CapacityRounding(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	storage := mocks.NewMockStorageBackend(logger, nil)
	svc := newExpandTestService(t, storage)

	const volumeID = "csi-pvc-round-test"
	_, err := storage.EnsureDiskCreated(context.Background(), volumeID, 1024, "standard", "se-sto-1")
	require.NoError(t, err)

	const reqBytes = 1*1024*1024*1024 + 1 // 1 GiB + 1 byte
	resp, err := svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId:      volumeID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: reqBytes},
	})

	require.NoError(t, err)
	require.Equal(t, int64(reqBytes), resp.CapacityBytes)

	// ceil(1 GiB + 1 byte / 1 MiB) = 1025 MiB
	disk, err := storage.GetDisk(context.Background(), volumeID)
	require.NoError(t, err)
	require.Equal(t, int32(1025), disk.Spec.DiskSize.Amount)
}

// A missing volume ID is rejected before the storage backend is called.
func TestControllerExpandVolume_MissingVolumeID(t *testing.T) {
	storage := mocks.NewMockStorageBackend(slog.Default(), nil)
	svc := newExpandTestService(t, storage)

	_, err := svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		CapacityRange: &csi.CapacityRange{RequiredBytes: 1024},
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// A missing capacity range is rejected.
func TestControllerExpandVolume_MissingCapacityRange(t *testing.T) {
	storage := mocks.NewMockStorageBackend(slog.Default(), nil)
	svc := newExpandTestService(t, storage)

	_, err := svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId: "some-vol",
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// A NotFound gRPC error from the backend is propagated as-is (not wrapped as
// Internal) and is not retried.
func TestControllerExpandVolume_StorageNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	storage := mocks.NewMockStorageBackend(logger, nil)
	_, err := storage.EnsureDiskCreated(context.Background(), "missing-vol", 1024, "standard", "se-sto-1")
	require.NoError(t, err)

	backend := &resizeErrorBackend{
		MockStorageBackend: storage,
		resizeErr:          status.Error(codes.NotFound, "disk missing-vol not found"),
	}
	svc := newExpandTestService(t, backend)

	_, err = svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId:      "missing-vol",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 2 * 1024 * 1024 * 1024},
	})

	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "not found")
}

// An OutOfRange error (shrink) from the backend is propagated as-is and is
// not retried or rewrapped as Internal.
func TestControllerExpandVolume_StorageOutOfRange(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	storage := mocks.NewMockStorageBackend(logger, nil)
	_, err := storage.EnsureDiskCreated(context.Background(), "shrink-vol", 4096, "standard", "se-sto-1")
	require.NoError(t, err)

	backend := &resizeErrorBackend{
		MockStorageBackend: storage,
		resizeErr:          status.Error(codes.OutOfRange, "disk cannot be shrunk"),
	}
	svc := newExpandTestService(t, backend)

	_, err = svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId:      "shrink-vol",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 1024 * 1024 * 1024},
	})

	require.Error(t, err)
	require.Equal(t, codes.OutOfRange, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "shrunk")
}

// A non-gRPC error from the backend is wrapped as Internal by the controller.
func TestControllerExpandVolume_StorageInternalError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	storage := mocks.NewMockStorageBackend(logger, nil)
	_, err := storage.EnsureDiskCreated(context.Background(), "err-vol", 1024, "standard", "se-sto-1")
	require.NoError(t, err)

	backend := &resizeErrorBackend{
		MockStorageBackend: storage,
		resizeErr:          fmt.Errorf("backend connection refused"),
	}
	svc := newExpandTestService(t, backend)

	_, err = svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId:      "err-vol",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 2 * 1024 * 1024 * 1024},
	})

	require.Error(t, err)
	// A plain (non-gRPC) error is not transient, so it is not retried, and
	// the controller wraps it as Internal.
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "failed to resize disk")
	require.Contains(t, status.Convert(err).Message(), "connection refused")
}
