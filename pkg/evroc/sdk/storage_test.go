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

// fakeAPI records requests and serves canned HotswapDiskAttachment responses.
type fakeAPI struct {
	listItems    []map[string]any
	getStatus    int
	deleteStatus int
	createStatus int
	createCalls  int
	listCalls    int
	getCalls     int
	deleteCalls  int
	deletedID    string
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

func (f *fakeAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "hotswapDiskAttachments") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

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
	})
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
		client:                client,
		identifier:            testProject,
		logger:                slog.New(slog.NewTextHandler(os.Stdout, nil)),
		metrics:               metrics.NewManager(),
		attachmentPollTimeout: 5 * time.Second,
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
