// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package identity

import (
	"context"
	"log/slog"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/version"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	driverName = "disk.csi.evroc.com"
)

// Service implements the CSI Identity Service.
type Service struct {
	csi.UnimplementedIdentityServer
	logger *slog.Logger
}

// NewIdentityService creates a new Identity Service.
func NewIdentityService(logger *slog.Logger) *Service {
	return &Service{
		logger: logger,
	}
}

// GetPluginInfo returns metadata about the plugin.
func (s *Service) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	s.logger.Debug("GetPluginInfo called")

	if driverName == "" {
		return nil, status.Error(codes.Unavailable, "driver name not configured")
	}

	return &csi.GetPluginInfoResponse{
		Name:          driverName,
		VendorVersion: version.Version,
	}, nil
}

// GetPluginCapabilities returns the capabilities of the plugin.
func (s *Service) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	s.logger.Debug("GetPluginCapabilities called")

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				// CONTROLLER_SERVICE indicates this driver implements the Controller service.
				// This means it can handle volume lifecycle operations (create, delete, attach, detach)
				// centrally, rather than requiring each node to manage volumes independently.
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				// VOLUME_ACCESSIBILITY_CONSTRAINTS indicates this driver is topology-aware.
				// This tells Kubernetes that:
				// - The driver understands zone/region topology
				// - Kubernetes should pass topology information (zones) in CreateVolume requests
				// - The scheduler should respect topology constraints when placing pods
				// - Volumes can only be accessed from nodes in specific zones
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

// Probe checks if the plugin is running.
func (s *Service) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	s.logger.Debug("Probe called")

	return &csi.ProbeResponse{
		Ready: wrapperspb.Bool(true),
	}, nil
}
