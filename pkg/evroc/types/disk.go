// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

//

package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Disk represents a volume in the evroc platform.
type Disk struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        EvrocMetadata `json:"metadata,omitempty"`
	Spec            DiskSpec      `json:"spec,omitempty"`
	Status          DiskStatus    `json:"status,omitempty"`
}

// Labels returns the userLabels for this disk.
func (d *Disk) Labels() map[string]string {
	return d.Metadata.GetLabels()
}

// DiskSpec defines the desired state of Disk.
type DiskSpec struct {
	DiskImage        *DiskImageInfo       `json:"diskImage,omitempty"`
	Location         ZonalLocation        `json:"location,omitempty"`
	DiskStorageClass DiskStorageClassInfo `json:"diskStorageClass"`
	DiskSize         DiskSize             `json:"diskSize,omitempty"`
}

// DiskSize specifies the size of the disk.
type DiskSize struct {
	Unit   Unit  `json:"unit"`
	Amount int32 `json:"amount"`
}

// DiskSizeOnStatus represents the disk size in status.
type DiskSizeOnStatus struct {
	Unit   Unit  `json:"unit"`
	Amount int32 `json:"amount"`
}

// DiskImageInfo contains the OS image information.
type DiskImageInfo struct {
	DiskImageRef DiskImageRef `json:"diskImageRef"`
}

// DiskImageRef references a DiskImage resource.
type DiskImageRef struct {
	// Name of the DiskImage (e.g., 'ubuntu.24-04.1', 'ubuntu.22-04.1').
	Name string `json:"name"`
}

// DiskStorageClassInfo contains storage class information.
type DiskStorageClassInfo struct {
	// Name of the DiskStorageClass (e.g., 'persistent').
	Name string `json:"name"`
}

// DiskStatus defines the observed state of Disk.
type DiskStatus struct {
	Location   ZonalLocationOnStatus `json:"location,omitempty"`
	Size       DiskSizeOnStatus      `json:"diskSize,omitempty"`
	Conditions ConditionsSet         `json:"conditions,omitempty"`
}

//

// DiskList contains a list of Disks.
type DiskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Disk `json:"items"`
}

// IsReady returns true if the disk is ready.
func (d *Disk) IsReady() bool {
	return d.Status.Conditions.IsReady()
}
