// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package evroc

import (
	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsAttachmentReady returns true if the attachment is fully reconciled and ready.
func IsAttachmentReady(attachment *types.HotswapDiskAttachment) bool {
	for _, cond := range attachment.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
