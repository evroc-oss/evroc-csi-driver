# Development Guide

This guide explains how to develop and test the evroc CSI driver.

## Prerequisites

- Go 1.24 or later
- Docker
- Kubernetes cluster (production or k3s for local testing)
- kubectl
- golangci-lint (for linting)

Nodes must have `blkid` (util-linux) and `mkfs.ext4` (e2fsprogs) installed.
See [README.md](../README.md) for installation instructions by distribution.

## Quick Start

### 1. Clone and Build

```bash
git clone https://github.com/evroc-oss/evroc-csi-driver.git
cd evroc-csi-driver
make build  # Build the binary
make test   # Run tests
make lint   # Run linter
```

### 2. Development Cluster Setup

Requirements:
- Kubernetes cluster with admin access
- All nodes labeled with `topology.kubernetes.io/zone`
- Access to evroc API credentials

**Single-node k3s setup:**

```bash
curl -sfL https://get.k3s.io | sh -
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown $(id -u):$(id -g) ~/.kube/config
export KUBECONFIG=$HOME/.kube/config
kubectl wait --for=condition=ready node --all --timeout=60s

# Label node with the actual evroc VM zone
kubectl label nodes --all topology.kubernetes.io/zone=<your-vm-zone>
```

**Remote cluster management** (from your local machine):

Ensure your VM's security group allows inbound traffic on port 6443.

```bash
scp evroc-user@<VM_IP>:/etc/rancher/k3s/k3s.yaml ~/.kube/evroc-config
sed -i 's/127.0.0.1/<VM_IP>/g' ~/.kube/evroc-config
export KUBECONFIG=$HOME/.kube/evroc-config
kubectl get nodes
```

### 3. Configuration Setup

```bash
cp deploy/kubernetes/examples/config.yaml /tmp/evroc-csi-config.yaml
# Edit with your credentials, then create the Secret:
kubectl create secret generic evroc-csi-config \
  --from-file=config.yaml=/tmp/evroc-csi-config.yaml \
  -n kube-system
rm /tmp/evroc-csi-config.yaml
```

### 4. Build and Load the Driver Image

```bash
make docker
```

**For k3s clusters** (import directly, no registry needed):
```bash
docker save ghcr.io/evroc-oss/evroc-csi-driver:latest | \
  ssh evroc-user@<VM_IP> "sudo k3s ctr images import -"
# For local k3s: sudo k3s ctr images import -
```

**For other clusters**, tag and push to an accessible registry:
```bash
docker tag ghcr.io/evroc-oss/evroc-csi-driver:latest your-registry.com/evroc-csi-driver:latest
docker push your-registry.com/evroc-csi-driver:latest
# Or: make push
```

### 5. Deploy the Driver

**Option 1: kubectl**
```bash
kubectl apply -f deploy/kubernetes/
```
_Update image references in `deploy/kubernetes/controller.yaml` and `deploy/kubernetes/daemonset.yaml` if using a custom registry._

**Option 2: Helm**
```bash
helm install evroc-csi-driver ./chart \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-csi-config
```

**Option 3: make**
```bash
make install
```

Verify:
```bash
kubectl get pods -n kube-system -l app.kubernetes.io/name=evroc-csi-driver
kubectl get csidriver
```

### 6. Test Volume Operations

```bash
kubectl apply -f deploy/kubernetes/examples/example-pvc.yaml
kubectl apply -f deploy/kubernetes/examples/example-pod.yaml
kubectl wait --for=condition=ready pod/evroc-test-pod --timeout=60s
kubectl exec evroc-test-pod -- df -h /data

# Cleanup
kubectl delete pod evroc-test-pod
kubectl delete pvc evroc-test-pvc
```

## Development Workflow

### Rapid Iteration with dev-reload

```bash
# Edit code, then:
make dev-reload
# This builds the image, pushes/reloads it, restarts pods, and waits for readiness.
```

### Viewing Logs

```bash
make logs  # All driver logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c evroc-csi-driver --tail=50 -f

# View node plugin logs from specific node
kubectl logs -n kube-system -l app=evroc-csi-driver -c evroc-csi-driver --tail=50 -f

# View sidecar logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-provisioner
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-attacher
kubectl logs -n kube-system -l app=evroc-csi-driver -c node-driver-registrar
```

## Project Structure

```
evroc-csi-driver/
├── cmd/                         # Entry point
├── pkg/
│   ├── driver/                  # gRPC server and service registration
│   ├── identity/                # Identity service
│   ├── controller/              # Controller service
│   ├── node/                    # Node service
│   ├── evroc/                   # REST API client and types
│   ├── auth/                    # OIDC authentication
│   ├── config/                  # Configuration loading
│   ├── filesystem/              # mkfs, mount, blkid operations
│   ├── metrics/                 # Prometheus metrics
│   └── version/                 # Version information
├── deploy/kubernetes/           # Raw Kubernetes manifests
├── test/                        # Unit, sanity, and e2e tests
├── chart/                       # Helm chart
└── docs/                        # Documentation
```

## Making Changes

### Adding a New RPC

Example: Adding volume expansion support.

1. **Implement in controller** (`pkg/controller/controller.go`):
   - Add `ControllerExpandVolume` RPC.
2. **Update capabilities** in the same file:
   - Add `csi.ControllerServiceCapability_RPC_EXPAND_VOLUME`.
3. **Implement storage backend method** (`pkg/evroc/rest/storage.go`):
   - Add `ExpandDisk` method.
4. **Test**:
   ```bash
   make dev-reload
   make test
   ```

### Updating Deployment Configuration

- **Controller:** `deploy/kubernetes/controller.yaml` — replicas, sidecars, env vars.
- **Node plugin:** `deploy/kubernetes/daemonset.yaml` — mounts, privileged mode.

After changes:
```bash
kubectl delete -f deploy/kubernetes/controller.yaml
kubectl delete -f deploy/kubernetes/daemonset.yaml
kubectl apply -f deploy/kubernetes/
```

## Testing

### Unit Tests

```bash
make test                              # Run all tests
go test -v -race -coverprofile=coverage.txt ./pkg/...
go tool cover -html=coverage.txt       # View report
go test -v ./pkg/controller/           # Specific package
```

### CSI Sanity Tests

Provided by the [CSI test](https://github.com/kubernetes-csi/csi-test) community.
They validate RPC compliance, idempotency, error handling, and lifecycle — without a real backend.

```bash
cd test/sanity
go test -v
```

### Integration Tests

Test against a real evroc backend:

```bash
kubectl apply -f deploy/kubernetes/
kubectl apply -f deploy/kubernetes/examples/example-pvc.yaml
kubectl apply -f deploy/kubernetes/examples/example-pod.yaml
kubectl get pvc
kubectl get volumeattachment
kubectl exec evroc-test-pod -- df -h /data

# Cleanup
kubectl delete -f deploy/kubernetes/examples/example-pod.yaml
kubectl delete -f deploy/kubernetes/examples/example-pvc.yaml
```

## Debugging

### Driver Pods Crash Loop

```bash
kubectl logs -n kube-system <pod-name> -c evroc-csi-driver
```
Common causes: missing/invalid config, OIDC failure, REST API unreachable.

### PVC Stuck in Pending

```bash
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-provisioner
kubectl logs -n kube-system -l app=evroc-csi-controller -c evroc-csi-driver
kubectl describe pvc <pvc-name>
```
Common causes: CreateVolume failing, REST API errors, zone topology missing.

### VolumeAttachment Fails

```bash
kubectl logs -n kube-system -l app=evroc-csi-controller -c csi-attacher
kubectl describe volumeattachment <attachment-name>
```
Common causes: VM not found in evroc, HotswapDiskAttachment creation failed.

### Volume Mount Fails on Node

```bash
# Check node plugin logs on the specific node
kubectl logs -n kube-system -l app=evroc-csi-driver -c evroc-csi-driver

# Check device availability
kubectl exec -n kube-system <node-pod> -- ls -la /dev/disk/by-id/
```
Common causes: device not attached, `mkfs.ext4`/`mount` not installed, permission issues.

### Pod Deletion Hangs

```bash
kubectl logs -n kube-system -l app=evroc-csi-driver -c evroc-csi-driver
```
Common causes: NodeUnpublishVolume failing, device busy.

### Enable Verbose Logging

Update sidecar arguments in `deploy/kubernetes/controller.yaml`:
```yaml
args:
  - "--csi-address=$(ADDRESS)"
  - "--v=5"  # Increase to 10 for very verbose logging
```

### Debugging with Delve

```bash
go build -gcflags="all=-N -l" -o bin/evroc-csi-driver cmd/evroc-csi-driver/main.go
dlv exec ./bin/evroc-csi-driver -- \
  --endpoint=unix:///tmp/csi.sock \
  --mode=all \
  --config=/path/to/config.yaml
```

### Common Development Issues

- **kubectl configuration:** After installing k3s, set `export KUBECONFIG=$HOME/.kube/config` (add to `~/.bashrc` for persistence). Without this, kubectl tries to read `/etc/rancher/k3s/k3s.yaml` which requires root.
- **Zone label mismatch:** The node label `topology.kubernetes.io/zone` **must** match the actual evroc VM zone, or disk attachment will fail. Use `kubectl label nodes <node-name> topology.kubernetes.io/zone=<actual-vm-zone> --overwrite`
- **Image access:** `ghcr.io/evroc-oss/evroc-csi-driver:latest` may not be publicly available. Build locally and import into k3s as shown in [Build and Load](#4-build-and-load-the-driver-image).
- **Disk pressure:** Small test VMs may have disk-pressure taints. Remove with: `kubectl taint nodes <node-name> node.kubernetes.io/disk-pressure:NoSchedule-`

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the driver binary |
| `make run` | Run the driver locally |
| `make test` | Run unit tests |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code with gofmt |
| `make vet` | Run go vet static analysis |
| `make clean` | Clean build artifacts |
| `make docker` | Build Docker image |
| `make push` | Push Docker image to registry |
| `make generate` | Run code generation (placeholder) |
| `make manifest` | View Kubernetes manifests |
| `make install` | Deploy to Kubernetes cluster |
| `make uninstall` | Remove from Kubernetes cluster |
| `make dev-reload` | Quick rebuild and reload in cluster |
| `make logs` | View driver logs from Kubernetes |
| `make verify` | Run all verification checks |

## Best Practices

- Follow Go best practices; use `gofmt` and pass `golangci-lint` with zero warnings.
- Write **idempotent** operations (safe to retry).
- Log all RPC calls at entry with key parameters.
- Return proper gRPC error codes (`codes.InvalidArgument`, `codes.Internal`, etc.).
- Wrap errors with context rather than returning raw errors.
- Record Prometheus metrics for all significant operations.

## Resources

- [CSI Specification](https://github.com/container-storage-interface/spec/blob/master/spec.md)
- [Kubernetes CSI Documentation](https://kubernetes-csi.github.io/docs/)
- [CSI Hostpath Driver Example](https://github.com/kubernetes-csi/csi-driver-host-path)
- [evroc REST API Documentation](https://docs.evroc.com/)

For security, signing, and SBOM verification details, see [README.md](../README.md).
