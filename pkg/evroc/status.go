// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package evroc

import (
	"github.com/evroc-oss/evroc-go-sdk/compute"
	computetypes "github.com/evroc-oss/evroc-go-sdk/types/compute"
)

// IsAttachmentReady returns true if the attachment is fully reconciled and ready.
func IsAttachmentReady(attachment *computetypes.HotswapDiskAttachment) bool {
	return compute.IsAttachmentReady(attachment)
}
