// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	evroc "github.com/evroc-oss/evroc-go-sdk"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testProject = "test-project"
	testRegion  = "se-sto"
	testDisk    = "csi-pvc-test-volume"
	testWorker  = "test-worker-1"
	testControl = "test-control-plane-1"
	// The SDK requires an unexpired JWT before sending requests.
	testToken = "eyJhbGciOiAibm9uZSIsICJ0eXAiOiAiSldUIn0." +
		"eyJleHAiOiAyMDAwMDAwMDAwLCAic3ViIjogInRlc3QifQ.sig"
)

// fakeAPI records requests and serves canned HotswapDiskAttachment and Disk
// responses. Disk responses are driven by a mutable fakeDisk so that tests
// can simulate backend reconciliation between requests.
type fakeAPI struct {
	// Attachment fields.
	listItems    []map[string]any
	getStatus    int
	deleteStatus int
	createStatus int
	createCalls  int
	listCalls    int
	getCalls     int
	deleteCalls  int
	deletedID    string

	// Disk fields.
	disk            *fakeDisk
	diskGetStatus   int // non-zero overrides the GET response status.
	diskPatchStatus int // non-zero overrides the PATCH response status.
	diskGetCalls    int
	diskPatchCalls  int
}

// fakeDisk is a mutable representation of a Disk returned by the fake API.
// Mutating it between requests simulates the backend reconciling the status
// to match the spec.
type fakeDisk struct {
	specAmount   int32
	specUnit     string
	statusAmount int32
	statusUnit   string
	statusNil    bool // when true, omit diskSize from the status.
	managedBy    string
}

func attachmentJSON(diskName, vmName string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"id":         attachmentName(diskName, vmName),
			"userLabels": map[string]string{managedByLabel: testProject},
		},
		"spec": map[string]any{
			"diskRef":           fmt.Sprintf("/compute/projects/%s/regions/%s/disks/%s", testProject, testRegion, diskName),
			"virtualMachineRef": fmt.Sprintf("/compute/projects/%s/regions/%s/virtualMachines/%s", testProject, testRegion, vmName),
		},
		"status": map[string]any{"serial": "test-disk-serial"},
	}
}

// diskJSON renders the fakeDisk as the JSON expected by the SDK's Disk type.
func diskJSON(d *fakeDisk) map[string]any {
	if d == nil {
		return map[string]any{}
	}
	spec := map[string]any{
		"diskSize": map[string]any{
			"amount": d.specAmount,
			"unit":   d.specUnit,
		},
		"placement": map[string]any{},
	}
	status := map[string]any{}
	if !d.statusNil {
		status["diskSize"] = map[string]any{
			"amount": d.statusAmount,
			"unit":   d.statusUnit,
		}
	}
	labels := map[string]string{}
	if d.managedBy != "" {
		labels[managedByLabel] = d.managedBy
	}
	return map[string]any{
		"metadata": map[string]any{
			"id":         testDisk,
			"userLabels": labels,
		},
		"spec":   spec,
		"status": status,
	}
}

func (f *fakeAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "hotswapDiskAttachments"):
			f.handleAttachment(w, r)
		case strings.Contains(r.URL.Path, "/disks"):
			f.handleDisk(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeAPI) handleAttachment(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		f.createCalls++
		status := f.createStatus
		if status == 0 {
			status = http.StatusCreated
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "conflict"})

	case http.MethodGet:
		isResourceGet := !strings.HasSuffix(r.URL.Path, "hotswapDiskAttachments")
		if isResourceGet {
			f.getCalls++
		}
		// WaitForDeleted addresses one attachment by ID. Once DELETE has
		// succeeded, report it absent.
		if f.deletedID != "" && strings.HasSuffix(r.URL.Path, "/"+f.deletedID) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if isResourceGet {
			if f.getStatus != 0 {
				w.WriteHeader(f.getStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(attachmentJSON(testDisk, testWorker))
			return
		}
		f.listCalls++
		items := f.listItems
		if items == nil {
			items = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})

	case http.MethodDelete:
		f.deleteCalls++
		if f.deleteStatus != 0 {
			w.WriteHeader(f.deleteStatus)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		f.deletedID = parts[len(parts)-1]
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleDisk serves Disk GET and PATCH requests. A successful PATCH
// reconciles the fake disk: the spec and status are updated to the patched
// size so that subsequent GETs report the new size, mirroring the backend.
func (f *fakeAPI) handleDisk(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		f.diskGetCalls++
		if f.diskGetStatus != 0 {
			w.WriteHeader(f.diskGetStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(diskJSON(f.disk))

	case http.MethodPatch:
		f.diskPatchCalls++
		if f.diskPatchStatus != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(f.diskPatchStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"reason": "patch failed"})
			return
		}
		// Apply the patch and reconcile the status to the patched size.
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if amount, ok := patchAmount(body); ok && f.disk != nil {
			f.disk.specAmount = amount
			f.disk.specUnit = "MB"
			f.disk.statusNil = false
			f.disk.statusAmount = amount
			f.disk.statusUnit = "MB"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(diskJSON(f.disk))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// patchAmount extracts spec.diskSize.amount from a PATCH request body
// (JSON numbers decode to float64).
func patchAmount(body map[string]any) (int32, bool) {
	spec, ok := body["spec"].(map[string]any)
	if !ok {
		return 0, false
	}
	ds, ok := spec["diskSize"].(map[string]any)
	if !ok {
		return 0, false
	}
	amt, ok := ds["amount"].(float64)
	if !ok {
		return 0, false
	}
	return int32(amt), true
}

func newTestBackend(t *testing.T, api *fakeAPI) *SDKStorageBackend {
	t.Helper()

	srv := httptest.NewServer(api.handler())
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.API.BaseURL = srv.URL
	cfg.Auth.Token = testToken
	cfg.Context.Project = testProject
	cfg.Context.Region = testRegion
	cfg.Context.Organization = "test-org"

	client, err := evroc.New(context.Background(), cfg.SDKConfig())
	require.NoError(t, err)

	return &SDKStorageBackend{
		client:                 client,
		identifier:             testProject,
		logger:                 slog.New(slog.NewTextHandler(os.Stdout, nil)),
		metrics:                metrics.NewManager(),
		attachmentPollTimeout:  5 * time.Second,
		diskResizePollTimeout:  5 * time.Second,
		diskResizePollInterval: 10 * time.Millisecond,
	}
}

// The reported production case: remove the single stale attachment, wait for
// deletion, and recreate it for the VM requested by Kubernetes.
func TestEnsureAttachmentCreated_RecreatesAttachmentFromOtherVM(t *testing.T) {
	api := &fakeAPI{listItems: []map[string]any{attachmentJSON(testDisk, testWorker)}}
	backend := newTestBackend(t, api)

	require.NoError(t, backend.EnsureAttachmentCreated(context.Background(), testDisk, testControl))

	require.Equal(t, 1, api.deleteCalls)
	require.Equal(t, attachmentName(testDisk, testWorker), api.deletedID)
	require.Equal(t, 1, api.createCalls)
}

// A disk showing more than one attachment is already inconsistent. Moving one
// would leave the disk reachable from two VMs, so the attach must fail instead.
func TestEnsureAttachmentCreated_RefusesWhenDiskHasSeveralAttachments(t *testing.T) {
	api := &fakeAPI{listItems: []map[string]any{
		attachmentJSON(testDisk, testWorker),
		attachmentJSON(testDisk, "test-worker-2"),
	}}
	backend := newTestBackend(t, api)

	err := backend.EnsureAttachmentCreated(context.Background(), testDisk, testControl)

	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Zero(t, api.deleteCalls, "must not delete when the disk has several attachments")
	require.Zero(t, api.createCalls, "must not create another attachment")
}

// The requested VM already holds the disk: publication is idempotent.
func TestEnsureAttachmentCreated_SameVMIsIdempotent(t *testing.T) {
	api := &fakeAPI{
		listItems:    []map[string]any{attachmentJSON(testDisk, testWorker)},
		createStatus: http.StatusConflict,
	}
	backend := newTestBackend(t, api)

	require.NoError(t, backend.EnsureAttachmentCreated(context.Background(), testDisk, testWorker))
	require.Equal(t, 1, api.createCalls)
}

func TestEnsureAttachmentDeleted_ForbiddenIsNotSuccess(t *testing.T) {
	for _, api := range []*fakeAPI{
		{getStatus: http.StatusForbidden},
		{deleteStatus: http.StatusForbidden},
	} {
		backend := newTestBackend(t, api)
		err := backend.EnsureAttachmentDeleted(context.Background(), testDisk, testWorker)
		require.Error(t, err)
	}
}

func TestEnsureAttachmentDeleted_WaitsForDeletion(t *testing.T) {
	api := &fakeAPI{}
	backend := newTestBackend(t, api)

	require.NoError(t, backend.EnsureAttachmentDeleted(context.Background(), testDisk, testWorker))
	require.Equal(t, attachmentName(testDisk, testWorker), api.deletedID)
	require.Equal(t, 2, api.getCalls, "must confirm deletion after DELETE")
}

// When the disk status already reports the requested size, the resize is a
// no-op: no PATCH request is sent.
func TestEnsureDiskResized_AlreadyAtRequestedSizeIsIdempotent(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount:   2048,
			specUnit:     "MB",
			statusAmount: 2048,
			statusUnit:   "MB",
			managedBy:    testProject,
		},
	}
	backend := newTestBackend(t, api)

	require.NoError(t, backend.EnsureDiskResized(context.Background(), testDisk, 2048))

	require.Equal(t, 1, api.diskGetCalls, "must GET the disk once")
	require.Zero(t, api.diskPatchCalls, "must not PATCH when already at requested size")
}

// Shrinking is rejected with codes.OutOfRange before any PATCH is sent.
func TestEnsureDiskResized_RejectsShrink(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount:   4096,
			specUnit:     "MB",
			statusAmount: 4096,
			statusUnit:   "MB",
			managedBy:    testProject,
		},
	}
	backend := newTestBackend(t, api)

	err := backend.EnsureDiskResized(context.Background(), testDisk, 2048)

	require.Error(t, err)
	require.Equal(t, codes.OutOfRange, status.Code(err))
	require.Zero(t, api.diskPatchCalls, "must not PATCH a shrink request")
}

// The shrink check converts the status size to MiB before comparing, so a
// disk reported in GB is correctly detected as larger than the request.
func TestEnsureDiskResized_RejectsShrinkWithGBUnit(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount:   2,
			specUnit:     "GB",
			statusAmount: 2,
			statusUnit:   "GB",
			managedBy:    testProject,
		},
	}
	backend := newTestBackend(t, api)

	// 2 GB = 2048 MB; requesting 1024 MB is a shrink.
	err := backend.EnsureDiskResized(context.Background(), testDisk, 1024)

	require.Error(t, err)
	require.Equal(t, codes.OutOfRange, status.Code(err))
	require.Zero(t, api.diskPatchCalls, "must not PATCH a shrink request")
}

// Growing a disk sends a PATCH and then polls GET until the status reflects
// the new size.
func TestEnsureDiskResized_GrowsAndPollsUntilReconciled(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount:   1024,
			specUnit:     "MB",
			statusAmount: 1024,
			statusUnit:   "MB",
			managedBy:    testProject,
		},
	}
	backend := newTestBackend(t, api)

	require.NoError(t, backend.EnsureDiskResized(context.Background(), testDisk, 2048))

	require.Equal(t, 1, api.diskPatchCalls, "must PATCH once to grow")
	require.GreaterOrEqual(t, api.diskGetCalls, 2, "must GET initially and at least once during poll")
}

// When the status has no DiskSize (nil), the shrink check is skipped and the
// grow proceeds normally.
func TestEnsureDiskResized_AllowsGrowWhenStatusIsNil(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount: 1024,
			specUnit:   "MB",
			statusNil:  true,
			managedBy:  testProject,
		},
	}
	backend := newTestBackend(t, api)

	require.NoError(t, backend.EnsureDiskResized(context.Background(), testDisk, 2048))

	require.Equal(t, 1, api.diskPatchCalls, "must PATCH once to grow")
}

// A GET failure (e.g. the disk does not exist) is propagated and no PATCH is
// attempted.
func TestEnsureDiskResized_GetFailureIsPropagated(t *testing.T) {
	api := &fakeAPI{
		diskGetStatus: http.StatusNotFound,
	}
	backend := newTestBackend(t, api)

	err := backend.EnsureDiskResized(context.Background(), testDisk, 2048)

	require.Error(t, err)
	require.Zero(t, api.diskPatchCalls, "must not PATCH when GET fails")
}

// A PATCH failure that is not a conflict is converted to a gRPC status via
// sdkErrorToGRPC.
func TestEnsureDiskResized_PatchFailureIsPropagated(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount:   1024,
			specUnit:     "MB",
			statusAmount: 1024,
			statusUnit:   "MB",
			managedBy:    testProject,
		},
		diskPatchStatus: http.StatusBadRequest,
	}
	backend := newTestBackend(t, api)

	err := backend.EnsureDiskResized(context.Background(), testDisk, 2048)

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, 1, api.diskPatchCalls)
}

// A PATCH conflict returns codes.Aborted so retryOnTransient requeues the
// resize; the top-of-function guards (idempotency, shrink, ownership) handle
// the retry.
func TestEnsureDiskResized_PatchConflictReturnsAborted(t *testing.T) {
	api := &fakeAPI{
		disk: &fakeDisk{
			specAmount:   2048,
			specUnit:     "MB",
			statusAmount: 1024,
			statusUnit:   "MB",
			managedBy:    testProject,
		},
		diskPatchStatus: http.StatusConflict,
	}
	backend := newTestBackend(t, api)

	err := backend.EnsureDiskResized(context.Background(), testDisk, 2048)

	require.Error(t, err)
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Equal(t, 1, api.diskPatchCalls, "must PATCH once (returns conflict)")
	require.Equal(t, 1, api.diskGetCalls, "must GET once via GetDisk before PATCH")
}
