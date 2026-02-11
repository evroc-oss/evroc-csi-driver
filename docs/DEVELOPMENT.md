# Development Guide

This guide explains how to develop and test the evroc CSI driver.

## Prerequisites

- Go 1.24 or later
- Docker
- Kubernetes cluster (production or k3s for local testing)
- kubectl
- golangci-lint (for linting)

### Node Runtime Requirements

Ensure the following packages are installed on **all Kubernetes worker nodes**:

**Debian/Ubuntu:**
```bash
sudo apt-get update
sudo apt-get install util-linux e2fsprogs
```

**RHEL/CentOS/Fedora:**
```bash
sudo yum install util-linux e2fsprogs
```

**Alpine Linux:**
```bash
sudo apk add util-linux e2fsprogs
```

## Quick Start

### 1. Clone and Build

```bash
# Clone the repository
git clone https://github.com/evroc-oss/evroc-csi-driver.git
cd evroc-csi-driver

# Build the binary
make build

# Run tests
make test

# Run linter
make lint
```

### 2. Development Cluster Setup

For development, use an existing Kubernetes cluster with the evroc CSI driver requirements:

**Requirements:**
- Kubernetes cluster with admin access
- All nodes labeled with `topology.kubernetes.io/zone`
- Access to evroc API credentials

**Quick setup for single-node testing:**

```bash
# If using k3s (on the VM)
curl -sfL https://get.k3s.io | sh -
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown $(id -u):$(id -g) ~/.kube/config
export KUBECONFIG=$HOME/.kube/config
kubectl wait --for=condition=ready node --all --timeout=60s

# Label node with the ACTUAL evroc VM zone (check your VM's zone first!)
kubectl label nodes --all topology.kubernetes.io/zone=<your-vm-zone>
```

**Remote cluster management:**

To manage the cluster from your local machine instead of SSH:

Prerequisites:
- Ensure your VM's security group allows inbound traffic on port 6443 (Kubernetes API)

```bash
# On your local machine
# Copy kubeconfig from VM (replace with your VM IP)
scp evroc-user@<VM_IP>:/etc/rancher/k3s/k3s.yaml ~/.kube/evroc-config

# Update the server URL to use VM's public IP
sed -i 's/127.0.0.1/<VM_IP>/g' ~/.kube/evroc-config

# Use the remote config
export KUBECONFIG=$HOME/.kube/evroc-config
kubectl get nodes
```

**Multi-zone testing:**
For multi-zone testing, use an existing multi-node cluster or cloud provider with multiple availability zones. Setting up multi-node clusters from scratch is beyond the scope of this guide.

### 3. Configuration Setup

Create a configuration Secret for the CSI driver:

```bash
# Copy example configuration
cp deploy/kubernetes/examples/config.yaml /tmp/evroc-csi-config.yaml

# Edit with your credentials
vim /tmp/evroc-csi-config.yaml

# Create Secret
kubectl create secret generic evroc-csi-config \
  --from-file=config.yaml=/tmp/evroc-csi-config.yaml \
  -n kube-system

# Cleanup plaintext file
rm /tmp/evroc-csi-config.yaml
```

### 4. Build and Load the Driver Image

**Note:** The driver image is not yet published to a public registry. You must build it locally.

Build the Docker image:
```bash
make docker
```

**For k3s clusters:**

Import the image directly into k3s (no registry needed):

```bash
# If k3s is on a remote VM
docker save ghcr.io/evroc-oss/evroc-csi-driver:latest | \
  ssh evroc-user@<VM_IP> "sudo k3s ctr images import -"

# If k3s is local
docker save ghcr.io/evroc-oss/evroc-csi-driver:latest | \
  sudo k3s ctr images import -
```

**For other Kubernetes clusters:**

Push to a registry accessible by your cluster:
```bash
# Tag for your registry
docker tag ghcr.io/evroc-oss/evroc-csi-driver:latest your-registry.com/evroc-csi-driver:latest

# Push to registry
docker push your-registry.com/evroc-csi-driver:latest
```

### 5. Deploy the Driver

**Option 1: Deploy with kubectl**

```bash
kubectl apply -f deploy/kubernetes/
```

If you pushed to a different registry, update the image reference in `deploy/kubernetes/controller.yaml` and `deploy/kubernetes/daemonset.yaml` first.

**Option 2: Deploy with Helm**

```bash
helm install evroc-csi-driver ./chart \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-csi-config
```

If you pushed to a different registry, override the image:

```bash
helm install evroc-csi-driver ./chart \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-csi-config \
  --set controller.image.repository=your-registry.com/evroc-csi-driver \
  --set node.image.repository=your-registry.com/evroc-csi-driver
```

Verify deployment:
```bash
kubectl get pods -n kube-system -l app.kubernetes.io/name=evroc-csi-driver
kubectl get csidriver
```

### 6. Test Volume Operations

```bash
# Create PVC
kubectl apply -f deploy/kubernetes/examples/example-pvc.yaml

# Create Pod using PVC
kubectl apply -f deploy/kubernetes/examples/example-pod.yaml

# Verify volume is mounted
kubectl wait --for=condition=ready pod/evroc-test-pod --timeout=60s
kubectl exec evroc-test-pod -- df -h /data

# Write test data
kubectl exec evroc-test-pod -- sh -c 'echo "Hello from evroc CSI" > /data/test.txt'
kubectl exec evroc-test-pod -- cat /data/test.txt

# Cleanup
kubectl delete pod evroc-test-pod
kubectl delete pvc evroc-test-pvc
```

## Development Workflow

### Rapid Iteration with dev-reload

When making changes to the driver code:

```bash
# Edit code
vim pkg/controller/controller.go

# Quick rebuild and reload
make dev-reload

# This will:
# 1. Build Docker image
# 2. Push to registry
# 3. Delete and restart all driver pods
# 4. Wait for pods to be ready
```

### Viewing Logs

```bash
# View all driver logs (follows)
make logs

# View controller logs specifically
kubectl logs -n kube-system -l app=evroc-csi-controller -c evroc-csi-driver --tail=50 -f

# View node plugin logs from specific node
kubectl logs -n kube-system -l app=evroc-csi-node -c evroc-csi-driver --tail=50 -f

# View sidecar logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-provisioner
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-attacher
kubectl logs -n kube-system -l app=evroc-csi-node -c node-driver-registrar
```

## Project Structure

```
evroc-csi-driver/
├── cmd/
│   └── evroc-csi-driver/
│       └── main.go              # Entry point: flag parsing, driver initialization
│
├── pkg/
│   ├── driver/
│   │   └── driver.go            # Main driver: gRPC server, service registration
│   │
│   ├── identity/
│   │   └── identity.go          # Identity service: GetPluginInfo, Probe
│   │
│   ├── controller/
│   │   └── controller.go        # Controller service: CreateVolume, DeleteVolume, etc.
│   │
│   ├── node/
│   │   └── node.go              # Node service: NodeStage, NodePublish, etc.
│   │
│   ├── evroc/
│   │   ├── rest/
│   │   │   ├── client.go        # HTTP client for REST API
│   │   │   ├── storage.go       # Disk and attachment operations
│   │   │   └── errors.go        # Error handling utilities
│   │   └── types/
│   │       ├── disk.go          # Disk resource types
│   │       ├── hotswap_disk_attachment.go  # Attachment types
│   │       └── common.go        # Shared types (zones, conditions)
│   │
│   ├── auth/
│   │   └── oidc.go              # OIDC authentication with token refresh
│   │
│   ├── config/
│   │   └── config.go            # Configuration loading and validation
│   │
│   ├── filesystem/
│   │   └── operations.go        # mkfs.ext4, mount, blkid operations
│   │
│   ├── metrics/
│   │   └── metrics.go           # Prometheus metrics collection
│   │
│   └── version/
│       └── version.go           # Version information
│
├── deploy/kubernetes/
│   ├── csidriver.yaml           # CSIDriver CRD registration
│   ├── rbac.yaml                # ServiceAccount, ClusterRole, ClusterRoleBinding
│   ├── controller.yaml          # Controller Deployment with sidecars
│   ├── daemonset.yaml           # Node DaemonSet with node-driver-registrar
│   ├── storageclass.yaml        # StorageClass definition
│   ├── metrics-service.yaml     # Prometheus metrics service
│   └── examples/
│       ├── config.yaml          # Example configuration file
│       ├── example-pvc.yaml     # Example PersistentVolumeClaim
│       └── example-pod.yaml     # Example Pod using PVC
│
├── test/
│   ├── sanity/
│   │   └── sanity_test.go       # CSI sanity tests
│   ├── e2e/
│   │   └── ...                  # End-to-end tests
│   └── mocks/
│       └── ...                  # Mock implementations for testing
│
├── chart/
│   ├── Chart.yaml               # Helm chart metadata
│   ├── values.yaml              # Helm chart default values
│   └── templates/               # Kubernetes resource templates
│
├── docs/
│   ├── ARCHITECTURE.md          # Architecture overview and diagrams
│   ├── CONFIGURATION.md         # Configuration guide
│   ├── DEVELOPMENT.md           # This file
│   └── SEQUENCE_DIAGRAMS.md     # Sequence diagrams for flows
│
├── scripts/                     # Helper scripts
├── tools/                       # Development tools
├── .golangci.yaml               # Linter configuration
├── .gitlab-ci.yml               # GitLab CI/CD pipeline
├── Dockerfile                   # Multi-stage Docker build
├── Makefile                     # Build automation
├── README.md                    # Project overview
├── CHANGELOG.md                 # Version history
├── CONTRIBUTING.md              # Contribution guidelines
├── LICENSE                      # Apache License 2.0
├── VERSION                      # Current version
├── go.mod                       # Go module definition
└── go.sum                       # Go module checksums
```

## Making Changes

### Adding a New RPC

Example: Adding volume expansion support:

1. **Implement in controller service** (`pkg/controller/controller.go`):

```go
func (s *Service) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
    s.logger.Info("ControllerExpandVolume called", "volumeID", req.GetVolumeId(), "capacity", req.GetCapacityRange().GetRequiredBytes())

    // Implement expansion logic
    // 1. Validate request
    // 2. Call storage backend to resize disk
    // 3. Return new capacity

    return &csi.ControllerExpandVolumeResponse{
        CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
    }, nil
}
```

2. **Update capabilities** (`pkg/controller/controller.go`):

```go
func (s *Service) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
    return &csi.ControllerGetCapabilitiesResponse{
        Capabilities: []*csi.ControllerServiceCapability{
            // ... existing capabilities
            makeControllerCapability(csi.ControllerServiceCapability_RPC_EXPAND_VOLUME),
        },
    }, nil
}
```

3. **Implement storage backend method** (`pkg/evroc/rest/storage.go`):

```go
func (c *Client) ExpandDisk(ctx context.Context, name string, newSizeMB int32) error {
    // Implement disk resize via PATCH request to REST API
}
```

4. **Test**:

```bash
make dev-reload
make test
```

### Updating Deployment Configuration

**Controller changes** (`deploy/kubernetes/controller.yaml`):
- Replicas, resource limits
- Sidecar image versions
- Environment variables
- Volume mounts

**Node plugin changes** (`deploy/kubernetes/daemonset.yaml`):
- Volume mounts (host paths)
- Privileged mode settings
- Host network configuration

**After changes**:

```bash
kubectl delete -f deploy/kubernetes/controller.yaml
kubectl delete -f deploy/kubernetes/daemonset.yaml
kubectl apply -f deploy/kubernetes/
```

## Testing

### Unit Tests

```bash
# Run all unit tests
make test

# Run with coverage
go test -v -race -coverprofile=coverage.txt ./pkg/...

# View coverage report
go tool cover -html=coverage.txt

# Run specific package tests
go test -v ./pkg/controller/
```

### CSI Sanity Tests

CSI sanity tests are provided by the Kubernetes CSI community (https://github.com/kubernetes-csi/csi-test) to verify that a CSI driver correctly implements the CSI specification. These tests run against the driver using mock storage backends and validate compliance with the CSI specification requirements.

```bash
# Run sanity tests
cd test/sanity
go test -v
```

What the sanity tests validate:
- **RPC implementation**: All required CSI RPCs are implemented and return correct responses
- **Idempotency**: Operations can be called multiple times with the same result
- **Error handling**: Proper gRPC error codes are returned for invalid requests
- **Volume lifecycle**: CreateVolume, ControllerPublishVolume, NodeStageVolume, NodePublishVolume sequence
- **Capability reporting**: Driver correctly advertises its capabilities

These tests do not require a real Kubernetes cluster or evroc backend.

### Integration Tests

Test against a real evroc backend:

```bash
# Ensure configuration is set up
kubectl get secret evroc-csi-config -n kube-system

# Deploy driver
kubectl apply -f deploy/kubernetes/

# Create PVC and pod
kubectl apply -f deploy/kubernetes/examples/example-pvc.yaml
kubectl apply -f deploy/kubernetes/examples/example-pod.yaml

# Verify volume operations
kubectl get pvc
kubectl get volumeattachment
kubectl exec evroc-test-pod -- df -h /data

# Cleanup
kubectl delete -f deploy/kubernetes/examples/example-pod.yaml
kubectl delete -f deploy/kubernetes/examples/example-pvc.yaml
```

## Debugging

### Common Issues

#### 1. Driver Pods Crash Loop

```bash
# Check logs
kubectl logs -n kube-system <pod-name> -c evroc-csi-driver

# Common causes:
# - Configuration missing or invalid
# - OIDC authentication failure
# - REST API unreachable
# - Socket directory doesn't exist (should auto-create)
```

#### 2. PVC Stuck in Pending

```bash
# Check provisioner logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-provisioner

# Check controller logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c evroc-csi-driver

# Check events
kubectl describe pvc <pvc-name>

# Common causes:
# - CreateVolume RPC failing
# - REST API errors (check metrics)
# - Zone topology not configured
# - Storage class misconfiguration
```

#### 3. Pod Can't Start (VolumeAttachment fails)

```bash
# Check attacher logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-attacher

# Check VolumeAttachment status
kubectl describe volumeattachment <attachment-name>

# Common causes:
# - ControllerPublishVolume failing
# - VM not found in evroc
# - HotswapDiskAttachment creation failed
```

#### 4. Volume Mount Fails on Node

```bash
# Check node plugin logs on the specific node
kubectl logs -n kube-system -l app=evroc-csi-node -c evroc-csi-driver

# Check device availability
kubectl exec -n kube-system <node-pod> -- ls -la /dev/disk/by-id/

# Common causes:
# - Device not attached to VM
# - mkfs.ext4 or mount binary not found
# - Incorrect device path
# - Permission issues
```

#### 5. Pod Deletion Hangs

```bash
# Check node plugin logs
kubectl logs -n kube-system -l app=evroc-csi-node -c evroc-csi-driver

# Common causes:
# - NodeUnpublishVolume failing
# - Device busy (process still using mount)
# - Unmount operation failing
```

### Enable Verbose Logging

Update sidecar arguments in `deploy/kubernetes/controller.yaml`:

```yaml
args:
  - "--csi-address=$(ADDRESS)"
  - "--v=5"  # Increase to 10 for very verbose logging
```

### Debugging with Delve

```bash
# Build with debug symbols
go build -gcflags="all=-N -l" -o bin/evroc-csi-driver cmd/evroc-csi-driver/main.go

# Run with delve
dlv exec ./bin/evroc-csi-driver -- \
  --endpoint=unix:///tmp/csi.sock \
  --mode=all \
  --config=/path/to/config.yaml
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the driver binary |
| `make test` | Run unit tests |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code with gofmt |
| `make vet` | Run go vet static analysis |
| `make clean` | Clean build artifacts |
| `make docker` | Build Docker image |
| `make push` | Push Docker image to registry |
| `make install` | Deploy to Kubernetes cluster |
| `make uninstall` | Remove from Kubernetes cluster |
| `make dev-reload` | Quick rebuild and reload in cluster |
| `make logs` | View driver logs from Kubernetes |
| `make verify` | Run all verification checks |

## Best Practices

### Code Style

- Follow Go best practices and idioms
- Use `gofmt` for formatting
- Pass `golangci-lint` with zero warnings
- Write idempotent operations (safe to retry)
- Log all RPC calls at entry with key parameters
- Return proper gRPC error codes (`codes.InvalidArgument`, `codes.Internal`, etc.)

### Error Handling

```go
// Bad - loses context
if err != nil {
    return nil, err
}

// Good - provides context and proper gRPC code
if err != nil {
    return nil, status.Errorf(codes.Internal, "failed to create disk: %v", err)
}
```

### Logging

```go
// Log RPC entry with key parameters
s.logger.Info("CreateVolume called", "name", req.GetName(), "size", size)

// Log important state changes
s.logger.Info("Volume created successfully", "volumeID", volumeID, "zone", zone)

// Log errors with full context
s.logger.Error("Failed to create disk", "name", name, "error", err)
```

### Idempotency

All CSI operations must be idempotent (safe to call multiple times):

```go
// Check if operation already completed
disk, err := s.storage.GetDisk(ctx, name)
if err == nil {
    // Disk already exists - validate it matches request
    if disk.Status.Size.Amount != expectedSize {
        return nil, status.Errorf(codes.AlreadyExists, "disk exists with different size")
    }
    // Return success (idempotent)
    return &csi.CreateVolumeResponse{Volume: ...}, nil
}
if !evroc.IsNotFound(err) {
    return nil, status.Errorf(codes.Internal, "failed to check disk: %v", err)
}

// Disk doesn't exist - create it
// ...
```

### Metrics

Record metrics for all significant operations:

```go
startTime := time.Now()

// ... perform operation ...

if err != nil {
    s.metrics.RecordVolumeOperationError("create", time.Since(startTime).Seconds(), "internal")
    return nil, err
}

s.metrics.RecordVolumeOperation("create", time.Since(startTime).Seconds())
```

## Common Development Issues

### kubectl Configuration

After installing k3s and copying the kubeconfig, you must set the `KUBECONFIG` environment variable:

```bash
export KUBECONFIG=$HOME/.kube/config
# Add to ~/.bashrc or ~/.zshrc for persistence
echo 'export KUBECONFIG=$HOME/.kube/config' >> ~/.bashrc
```

Without this, kubectl will try to read `/etc/rancher/k3s/k3s.yaml` which requires root permissions.

### Zone Label Must Match VM Zone

The zone label on your Kubernetes node **must match** the actual zone where your evroc VM is deployed. If there's a mismatch, disk attachment will fail with:

```
Disk zone does not match VM, expected: c, found: a
```

To find your VM's zone, check the evroc console or API. Then label your node:

```bash
kubectl label nodes <node-name> topology.kubernetes.io/zone=<actual-vm-zone> --overwrite
```

### Image Build Required for Testing

The image `ghcr.io/evroc-oss/evroc-csi-driver:latest` may not be publicly available. For local testing, build and load the image:

```bash
# On your development machine
docker build -t ghcr.io/evroc-oss/evroc-csi-driver:latest .

# Transfer to k3s
docker save ghcr.io/evroc-oss/evroc-csi-driver:latest | \
  ssh user@node "sudo k3s ctr images import -"
```

### Disk Pressure on Test VMs

Small test VMs may have disk pressure taints that prevent pod scheduling:

```bash
# Remove disk pressure taint if needed
kubectl taint nodes <node-name> node.kubernetes.io/disk-pressure:NoSchedule-
```

## Resources

- [CSI Specification](https://github.com/container-storage-interface/spec/blob/master/spec.md)
- [Kubernetes CSI Documentation](https://kubernetes-csi.github.io/docs/)
- [CSI Hostpath Driver Example](https://github.com/kubernetes-csi/csi-driver-host-path)
- [evroc REST API Documentation](https://docs.evroc.com/)
