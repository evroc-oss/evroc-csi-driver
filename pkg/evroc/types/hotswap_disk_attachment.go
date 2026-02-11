// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


//

package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HotswapDiskAttachment represents the hotplug attachment of a Disk to a VirtualMachine.
// Creating a HotswapDiskAttachment will trigger a hotplug operation to attach the
// referenced Disk to the referenced VirtualMachine. Deleting the HotswapDiskAttachment
// will detach the Disk from the VirtualMachine.
type HotswapDiskAttachment struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        EvrocMetadata                 `json:"metadata,omitempty"`
	Spec            HotswapDiskAttachmentSpec     `json:"spec,omitempty"`
	Status          HotswapDiskAttachmentStatus   `json:"status,omitempty"`
}

// Labels returns the userLabels for this attachment.
func (h *HotswapDiskAttachment) Labels() map[string]string {
	return h.Metadata.GetLabels()
}

// HotswapDiskAttachmentSpec defines the desired state of a HotswapDiskAttachment.
type HotswapDiskAttachmentSpec struct {
	// DiskRef is the name of the Disk to attach.
	DiskRef string `json:"diskRef"`

	// VMRef is the name of the VirtualMachine to attach the Disk to.
	VMRef string `json:"vmRef"`
}

// HotswapDiskAttachmentStatus defines the observed state of HotswapDiskAttachment.
type HotswapDiskAttachmentStatus struct {
	Serial     string             `json:"serial,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//

// HotswapDiskAttachmentList contains a list of HotswapDiskAttachments.
type HotswapDiskAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HotswapDiskAttachment `json:"items"`
}
