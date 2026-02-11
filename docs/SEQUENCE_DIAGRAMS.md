# evroc CSI Driver - Sequence Diagrams

This document shows the interaction flows in the evroc CSI driver for common volume operations.

## 1. Volume Provisioning & Pod Startup (Complete Flow)

This shows the end-to-end flow from creating a PVC to running a pod with a mounted volume.

```mermaid
sequenceDiagram
    actor User
    participant K8s as Kubernetes API
    participant Prov as csi-provisioner<br/>(sidecar)
    participant Ctrl as CSI Controller
    participant REST as evroc REST API
    participant Att as csi-attacher<br/>(sidecar)
    participant Kubelet as kubelet
    participant Node as CSI Node Plugin

    Note over User,REST: Phase 1: Volume Creation
        User->>K8s: kubectl apply -f pvc.yaml<br/>(request 10Gi volume)
        K8s->>K8s: Create PVC object<br/>(status: Pending)
        K8s-->>Prov: Watch: New PVC
        Prov->>Ctrl: CreateVolume(name, size=10Gi, zone=a)
        Ctrl->>REST: POST /apis/compute/v1alpha2/.../disks
        Note over REST: Create Disk resource<br/>with zone placement
        REST-->>Ctrl: Disk created
        Ctrl->>REST: GET /apis/compute/v1alpha2/.../disks/{name}
        Note over REST: Wait for Ready condition
        REST-->>Ctrl: Disk Ready
        Ctrl-->>Prov: Volume created<br/>(volumeId: pvc-abc123)
        Prov->>K8s: Create PersistentVolume
        K8s->>K8s: Bind PVC ↔ PV
        K8s-->>User: PVC status: Bound

    Note over User,Kubelet: Phase 2: Pod Scheduling
        User->>K8s: kubectl apply -f pod.yaml<br/>(uses PVC)
        K8s->>K8s: Schedule Pod to Node
        Note over K8s: Selected: worker-1 (zone a)

    Note over K8s,REST: Phase 3: Volume Attachment
        K8s->>K8s: Create VolumeAttachment<br/>(volume + node)
        K8s-->>Att: Watch: New VolumeAttachment
        Att->>Ctrl: ControllerPublishVolume<br/>(volumeId, nodeId)
        Ctrl->>REST: POST /apis/compute/v1alpha2/.../hotswap-disk-attachments
        Note over REST: Create HotswapDiskAttachment<br/>attaching disk to VM
        REST-->>Ctrl: Attachment created
        Ctrl->>REST: GET /apis/compute/v1alpha2/.../hotswap-disk-attachments/{name}
        Note over REST: Wait for Ready condition<br/>Get serial number
        REST-->>Ctrl: Attachment Ready<br/>serial: DISK123
        Ctrl-->>Att: PublishContext:<br/>{serial: DISK123}
        Att->>K8s: Update VolumeAttachment<br/>status: Attached

    Note over Kubelet,Node: Phase 4: Volume Staging
        Kubelet->>Node: NodeStageVolume<br/>(volumeId, serial, stagingPath)
        Note over Node: Find device by serial<br/>/dev/disk/by-id/virtio-DISK123
        Node->>Node: Run blkid to check filesystem
        alt No filesystem detected
            Node->>Node: mkfs.ext4 /dev/disk/by-id/virtio-DISK123
        end
        Node->>Node: mount /dev/... /var/lib/kubelet/.../staging/
        Node-->>Kubelet: Success

    Note over Kubelet,Node: Phase 5: Volume Publishing
        Kubelet->>Node: NodePublishVolume<br/>(stagingPath, targetPath)
        Node->>Node: Bind mount:<br/>/var/lib/kubelet/.../staging<br/>→ /var/lib/kubelet/.../mount
        Node-->>Kubelet: Success

    Note over User,Kubelet: Phase 6: Pod Starts
        Kubelet->>Kubelet: Start container with<br/>volume mounted at /data
        Kubelet-->>User: Pod status: Running
```

**Key Points**:
- **CreateVolume**: Creates actual Disk resource via REST API with zone placement
- **ControllerPublish**: Creates HotswapDiskAttachment to attach disk to VM
- **NodeStage**: Formats device with ext4 and mounts at staging path
- **NodePublish**: Bind-mounts from staging to pod target path
- Pod has real ext4 filesystem mounted at `/data`

---

## 2. Volume Deletion (Cleanup Flow)

This shows what happens when a user deletes a PVC.

```mermaid
sequenceDiagram
    actor User
    participant K8s as Kubernetes API
    participant Kubelet as kubelet
    participant Node as CSI Node Plugin
    participant Att as csi-attacher<br/>(sidecar)
    participant Ctrl as CSI Controller
    participant REST as evroc REST API
    participant Prov as csi-provisioner<br/>(sidecar)

    Note over User,Node: Phase 1: Pod Deletion
        User->>K8s: kubectl delete pod
        K8s->>Kubelet: Delete Pod
        Kubelet->>Node: NodeUnpublishVolume<br/>(targetPath)
        Node->>Node: Unmount bind mount<br/>/var/lib/kubelet/.../mount
        Node-->>Kubelet: Success

    Note over Kubelet,Node: Phase 2: Volume Unstaging
        Kubelet->>Node: NodeUnstageVolume<br/>(stagingPath)
        Node->>Node: Unmount filesystem<br/>/var/lib/kubelet/.../staging
        Node-->>Kubelet: Success

    Note over K8s,REST: Phase 3: Volume Detachment
        K8s->>K8s: Delete VolumeAttachment
        K8s-->>Att: Watch: VolumeAttachment deleted
        Att->>Ctrl: ControllerUnpublishVolume<br/>(volumeId, nodeId)
        Ctrl->>REST: DELETE /apis/compute/v1alpha2/.../hotswap-disk-attachments/{name}
        Note over REST: Delete HotswapDiskAttachment<br/>Detach disk from VM
        REST-->>Ctrl: Attachment deleted
        Ctrl-->>Att: Success

    Note over User,REST: Phase 4: Volume Deletion
        User->>K8s: kubectl delete pvc
        K8s->>K8s: Check reclaim policy<br/>(Delete)
        K8s-->>Prov: Watch: PVC deleted
        Prov->>Ctrl: DeleteVolume(volumeId)
        Ctrl->>REST: DELETE /apis/compute/v1alpha2/.../disks/{name}
        Note over REST: Delete Disk resource
        REST-->>Ctrl: Disk deleted
        Ctrl-->>Prov: Success
        Prov->>K8s: Delete PersistentVolume
        K8s-->>User: PVC deleted
```

**Cleanup Timing**:
- NodeUnpublish: Immediate (unmount bind mount)
- NodeUnstage: Immediate (unmount filesystem)
- ControllerUnpublish: ~1-5 seconds (delete attachment via REST API)
- DeleteVolume: ~5-10 seconds (delete disk via REST API)
- PV deletion: Up to 30 seconds (async garbage collection)

---

## 3. Read-Only Volume Mount (ROX Access Mode)

This shows how multiple pods can mount the same volume as read-only.

```mermaid
sequenceDiagram
    participant Kubelet1 as kubelet (node-1)
    participant Node1 as CSI Node (node-1)
    participant Kubelet2 as kubelet (node-2)
    participant Node2 as CSI Node (node-2)

    Note over Kubelet1,Node2: Same volume attached to both nodes

    Note over Kubelet1,Node1: Pod 1 on Node 1
        Kubelet1->>Node1: NodeStageVolume<br/>(volumeId, stagingPath)
        Node1->>Node1: mount -o ro /dev/... /staging
        Node1-->>Kubelet1: Success
        Kubelet1->>Node1: NodePublishVolume<br/>(stagingPath, targetPath, readonly=true)
        Node1->>Node1: Bind mount with ro flag
        Node1-->>Kubelet1: Success

    Note over Kubelet2,Node2: Pod 2 on Node 2
        Kubelet2->>Node2: NodeStageVolume<br/>(volumeId, stagingPath)
        Node2->>Node2: mount -o ro /dev/... /staging
        Node2-->>Kubelet2: Success
        Kubelet2->>Node2: NodePublishVolume<br/>(stagingPath, targetPath, readonly=true)
        Node2->>Node2: Bind mount with ro flag
        Node2-->>Kubelet2: Success

    Note over Kubelet1,Node2: Both pods have read-only access
```

**Key Points**:
- ROX (ReadOnlyMany) only supported for filesystem volumes
- Each node mounts the device read-only
- Multiple pods across nodes can read simultaneously
- Block volumes don't support ROX (would require loop devices)

---

## 4. OIDC Authentication Flow

This shows how the driver authenticates with evroc platform.

```mermaid
sequenceDiagram
    participant Driver as CSI Controller
    participant Auth as Auth Client
    participant OIDC as OIDC Provider<br/>(authn.iam.evroc.com)
    participant REST as REST API

    Note over Driver,OIDC: Driver Startup
        Driver->>Auth: Initialize(username, password)
        Auth->>OIDC: POST /token<br/>(grant_type=password)
        Note over OIDC: Validate credentials
        OIDC-->>Auth: access_token, refresh_token<br/>expires_in: 3600
        Auth-->>Driver: Ready

    Note over Driver,REST: API Call
        Driver->>Auth: GetAccessToken()
        alt Token valid
            Auth-->>Driver: Return cached token
        else Token expired
            Auth->>OIDC: POST /token<br/>(grant_type=refresh_token)
            OIDC-->>Auth: New access_token
            Auth-->>Driver: Return new token
        end
        Driver->>REST: GET /apis/compute/v1alpha2/...<br/>Authorization: Bearer {token}
        REST-->>Driver: Response

    Note over Auth,OIDC: Background Token Refresh
        loop Every 50 minutes (before expiry)
            Auth->>OIDC: POST /token<br/>(grant_type=refresh_token)
            OIDC-->>Auth: New access_token, refresh_token
            Auth->>Auth: Update cached token
        end
```

**Security Features**:
- Automatic token refresh before expiration
- Tokens cached in memory only (never persisted)
- Thread-safe token management
- Automatic retry on authentication failures

---

## 5. Topology-Aware Volume Provisioning

This shows how zone information flows for multi-zone deployments.

```mermaid
sequenceDiagram
    actor User
    participant K8s as Kubernetes API
    participant Scheduler as kube-scheduler
    participant Prov as csi-provisioner
    participant Ctrl as CSI Controller
    participant REST as evroc REST API

    Note over User,REST: Volume Provisioning with Topology

    User->>K8s: Create PVC with<br/>WaitForFirstConsumer StorageClass
    K8s->>K8s: Create PVC (Pending)

    User->>K8s: Create Pod using PVC
    Scheduler->>Scheduler: Evaluate node constraints<br/>Check node topology labels
    Note over Scheduler: Selected: node-3<br/>topology.kubernetes.io/zone=b
    Scheduler->>K8s: Bind Pod to node-3

    K8s->>Prov: Provision volume for node-3
    Note over Prov: Read node topology:<br/>zone=b
    Prov->>Ctrl: CreateVolume(<br/>  name, size,<br/>  accessibilityRequirements: {zone: b}<br/>)
    Ctrl->>REST: POST /apis/compute/v1alpha2/.../disks<br/>{ placement: { zone: "b" } }
    Note over REST: Create Disk in zone B
    REST-->>Ctrl: Disk created in zone B
    Ctrl-->>Prov: volumeId, topology: {zone: b}
    Prov->>K8s: Create PV with<br/>nodeAffinity: {zone: b}
    K8s->>K8s: Bind PVC ↔ PV

    Note over User,REST: Volume and Pod in same zone
```

**Key Points**:
- `WaitForFirstConsumer` delays provisioning until pod is scheduled
- Kubernetes scheduler selects node based on pod requirements
- CSI provisioner reads node's `topology.kubernetes.io/zone` label
- Controller creates disk in same zone as node
- Ensures pod and volume are co-located for best performance

---

## 6. Volume Statistics Reporting

This shows how volume usage metrics are collected.

```mermaid
sequenceDiagram
    participant Kubelet as kubelet
    participant Node as CSI Node Plugin
    participant FS as Filesystem

    loop Periodic (every 1 minute)
        Kubelet->>Node: NodeGetVolumeStats(volumeId, volumePath)
        Node->>FS: statfs(volumePath)
        Note over FS: Get filesystem stats:<br/>- Total bytes<br/>- Available bytes<br/>- Used bytes<br/>- Total inodes<br/>- Free inodes
        FS-->>Node: Filesystem statistics
        Node-->>Kubelet: VolumeStats{<br/>  capacityBytes,<br/>  availableBytes,<br/>  usedBytes,<br/>  totalInodes,<br/>  freeInodes<br/>}
        Kubelet->>Kubelet: Update volume metrics<br/>in Kubelet API
    end

    Note over Kubelet: Metrics available via<br/>kubectl top pvc
```

**Metrics Available**:
- Total capacity in bytes
- Available space in bytes
- Used space in bytes
- Total inodes
- Available inodes
- Used inodes

---

## 7. Error Handling & Retry Logic

This shows exponential backoff retry for transient errors.

```mermaid
sequenceDiagram
    participant Prov as csi-provisioner
    participant Ctrl as CSI Controller
    participant REST as evroc REST API

    Prov->>Ctrl: CreateVolume(name, size)

    Note over Ctrl,REST: Attempt 1
    Ctrl->>REST: POST /apis/.../disks
    REST-->>Ctrl: 503 Service Unavailable
    Note over Ctrl: Transient error detected<br/>Wait 1 second

    Note over Ctrl,REST: Attempt 2 (retry)
    Ctrl->>REST: POST /apis/.../disks
    REST-->>Ctrl: 503 Service Unavailable
    Note over Ctrl: Wait 2 seconds (exponential backoff)

    Note over Ctrl,REST: Attempt 3 (retry)
    Ctrl->>REST: POST /apis/.../disks
    REST-->>Ctrl: 201 Created
    Ctrl->>REST: GET /apis/.../disks/{name}
    REST-->>Ctrl: Disk Ready

    Ctrl-->>Prov: Success (volumeId)
```

**Retry Strategy**:
- Max retries: 3 attempts
- Initial delay: 1 second
- Max delay: 10 seconds
- Exponential backoff (1s → 2s → 4s → 8s → 10s cap)
- Only retries transient errors (5xx, timeouts)
- Permanent errors (4xx) fail immediately

---

## 8. Idempotent Operations

This shows how the driver handles duplicate requests safely.

```mermaid
sequenceDiagram
    participant Prov as csi-provisioner
    participant Ctrl as CSI Controller
    participant REST as evroc REST API

    Note over Prov,REST: First Request
    Prov->>Ctrl: CreateVolume(name, size=10Gi)
    Ctrl->>REST: POST /apis/.../disks
    REST-->>Ctrl: 201 Created
    Ctrl-->>Prov: Success (volumeId)

    Note over Prov,REST: Duplicate Request (retry)
    Prov->>Ctrl: CreateVolume(name, size=10Gi)
    Ctrl->>REST: POST /apis/.../disks
    REST-->>Ctrl: 409 Conflict (already exists)
    Ctrl->>REST: GET /apis/.../disks/{name}
    REST-->>Ctrl: Disk details (size=10Gi)
    Note over Ctrl: Validate disk matches request:<br/>✓ Size matches<br/>✓ Owned by this driver
    Ctrl-->>Prov: Success (volumeId)<br/>(idempotent response)

    Note over Prov,REST: Conflicting Request
    Prov->>Ctrl: CreateVolume(name, size=20Gi)
    Ctrl->>REST: POST /apis/.../disks
    REST-->>Ctrl: 409 Conflict (already exists)
    Ctrl->>REST: GET /apis/.../disks/{name}
    REST-->>Ctrl: Disk details (size=10Gi)
    Note over Ctrl: Size mismatch detected:<br/>Requested: 20Gi<br/>Existing: 10Gi
    Ctrl-->>Prov: Error: AlreadyExists<br/>(disk exists with different size)
```

**Idempotency Guarantees**:
- Duplicate requests with same parameters return success
- Conflicting requests (different parameters) return error
- All operations safe to retry
- Ownership validation prevents cross-cluster conflicts

---

## Summary

These diagrams illustrate the production CSI driver implementation with:

1. **Real Storage Operations**: Disk and HotswapDiskAttachment resources via REST API
2. **OIDC Authentication**: Secure authentication with automatic token refresh
3. **Filesystem Operations**: ext4 formatting, mounting, and statistics
4. **Topology Awareness**: Zone-based volume placement
5. **Error Handling**: Exponential backoff retry for transient errors
6. **Idempotency**: Safe retry behavior for all operations
7. **Multi-Zone Support**: Co-located volumes and pods for best performance

All operations integrate with the evroc platform via REST API (v1alpha2) for production-ready persistent storage.
