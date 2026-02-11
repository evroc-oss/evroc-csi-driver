// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


//

package types

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EvrocMetadata represents the metadata structure returned by the evroc API.
// The evroc API uses userLabels and systemLabels instead of Kubernetes' labels field.
// v1alpha2 uses 'id' instead of 'name' for resource identification.
type EvrocMetadata struct {
	ID                string            `json:"id"`
	UserLabels        map[string]string `json:"userLabels,omitempty"`
	SystemLabels      map[string]string `json:"systemLabels,omitempty"`
	UID               string            `json:"uid,omitempty"`
	CreationTimestamp *time.Time        `json:"creationTimestamp,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	Project           string            `json:"project,omitempty"`
	Region            string            `json:"region,omitempty"`
}

// GetLabels returns userLabels for compatibility with label-based operations.
func (m *EvrocMetadata) GetLabels() map[string]string {
	if m.UserLabels == nil {
		return make(map[string]string)
	}
	return m.UserLabels
}

// SetLabels sets userLabels for compatibility with label-based operations.
func (m *EvrocMetadata) SetLabels(labels map[string]string) {
	m.UserLabels = labels
}

// RegionName represents an evroc region identifier.
type RegionName string

// ExternalZoneName represents an evroc zone identifier.
type ExternalZoneName string

// Unit represents size units for disk sizing.
type Unit string

const (
	UnitKB Unit = "KB"
	UnitMB Unit = "MB"
	UnitGB Unit = "GB"
	UnitTB Unit = "TB"
)

// ZonalLocation specifies the required location for a zonal resource.
type ZonalLocation struct {
	// The required region for the resource.
	Region RegionName `json:"region,omitempty"`

	// The required zone within the region.
	Zone ExternalZoneName `json:"zone,omitempty"`
}

// ZonalLocationOnStatus represents the location in status.
type ZonalLocationOnStatus struct {
	Region RegionName       `json:"region,omitempty"`
	Zone   ExternalZoneName `json:"zone,omitempty"`
}

// ConditionsSet is an abstraction over []metav1.Condition.
type ConditionsSet []metav1.Condition

const (
	ConditionTypeReady = "Ready"

	ConditionReadyReasonCreating           = "Creating"
	ConditionReadyReasonDeleting           = "Deleting"
	ConditionReadyReasonProvisioningFailed = "ProvisioningFailed"
)

// IsReady returns true if the Ready condition is True.
func (cs *ConditionsSet) IsReady() bool {
	for i := range *cs {
		c := (*cs)[i]
		if c.Type == ConditionTypeReady {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}
