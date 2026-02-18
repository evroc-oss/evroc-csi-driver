<div align="center">
  <img src="docs/images/evroc-logo.png" alt="evroc" width="300"/>
</div>

# evroc CSI Driver

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/evroc-oss/evroc-csi-driver)](https://goreportcard.com/report/github.com/evroc-oss/evroc-csi-driver)

A Container Storage Interface (CSI) driver for evroc, implementing the CSI specification with full storage backend integration using the evroc REST API.

## Overview

This CSI driver implements all three CSI services:
- **Identity Service** - Returns plugin information and capabilities
- **Controller Service** - Handles volume lifecycle (create, delete, attach, detach) via evroc Disk and HotSwapDiskAttachment resources
- **Node Service** - Handles volume mounting/unmounting on nodes with ext4 filesystem support

The driver integrates with evroc's REST API to manage persistent storage volumes and their attachments to virtual machines.

## Features

- Full CSI specification compliance (v1.12.0)
- evroc REST API integration for volume and attachment management
- OIDC/OAuth2 authentication with automatic token refresh
- ext4 filesystem support with device formatting
- Volume staging and publishing with proper mount handling
- Topology awareness for zone-based scheduling
- Comprehensive logging of all CSI operations
- Kubernetes integration with DaemonSet deployment
- Multi-stage Docker build for minimal image size

## Prerequisites

### Development Requirements

- Go 1.24 or later
- Docker (for containerized deployment)
- Kubernetes cluster (for deployment testing)
- kubectl configured to access your cluster
- [golangci-lint](https://golangci-lint.run/usage/install/) (for linting)

### Node Runtime Requirements

**Important:** Kubernetes nodes must be evroc VMs. The CSI driver attaches evroc disks to nodes via the evroc REST API, which requires nodes to be running as VMs within the evroc platform.

The CSI driver requires the following system utilities to be available on **each Kubernetes node** where volumes will be mounted:

#### Required Packages

- **`blkid`** - Block device identification tool (from `util-linux` package)
  - Used to detect filesystem signatures on block devices
  - Most distributions: install `util-linux`

- **`mkfs.ext4`** - ext4 filesystem creation utility (from `e2fsprogs` package)
  - Required for formatting new volumes
  - Debian/Ubuntu: `sudo apt-get install e2fsprogs`
  - RHEL/CentOS/Fedora: `sudo yum install e2fsprogs`
  - Alpine: `sudo apk add e2fsprogs`

#### Installation by Distribution

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

**Note:** Most Linux distributions include `util-linux` by default, but `e2fsprogs` may need to be installed separately.

## Quick Start

### Development

1. **Build the driver:**
   ```bash
   make build
   ```

2. **Run tests:**
   ```bash
   make test
   ```

3. **Run linters:**
   ```bash
   make lint
   ```
   Note: Requires [golangci-lint](https://golangci-lint.run/usage/install/) to be installed.

### Installation (Production)

#### Prerequisites

- Kubernetes 1.28.0+
- Helm 3.0+ (for Helm installation)
- Kubernetes nodes must be evroc VMs
- Nodes must have `topology.kubernetes.io/zone` label set to one of: `a`, `b`, or `c`
- evroc platform credentials (username and password)
    - Request a service account from evroc support.

#### Step 1: Label Your Nodes

Each Kubernetes node must have a zone label. Set it to one of the valid zones:

```bash
kubectl label node <node-name> topology.kubernetes.io/zone=a
```

Valid zone values: `a`, `b`, or `c`

#### Step 2: Create Configuration Secret

Create a configuration file with your evroc credentials:

```bash
# Create config.yaml
cat > config.yaml <<EOF
evroc:
  organization: "your-org-id"
  project: "your-project-id"

auth:
  username: "service-account@evroc.com"
  password: "your-password"

infrastructure:
  region: "se-sto"
EOF

# Create the secret in kube-system namespace
kubectl create secret generic evroc-credentials \
  --namespace=kube-system \
  --from-file=config.yaml=config.yaml

# Clean up the config file
rm config.yaml
```

#### Step 3: Install via Helm

Install the CSI driver using Helm:

```bash
helm install evroc-csi-driver \
  https://github.com/evroc-oss/evroc-csi-driver/releases/download/v0.1.0/evroc-csi-driver-0.1.0.tgz \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-credentials
```

**Optional: Verify Helm chart signature before installation**

```bash
# Install Cosign (if not already installed)
# See: https://docs.sigstore.dev/cosign/installation/

# Download chart and signature files
VERSION=0.1.0
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz.sig
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz.pem

# Verify the signature (keyless)
cosign verify-blob evroc-csi-driver-${VERSION}.tgz \
  --signature evroc-csi-driver-${VERSION}.tgz.sig \
  --certificate evroc-csi-driver-${VERSION}.tgz.pem \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"

# Install the verified chart
helm install evroc-csi-driver ./evroc-csi-driver-${VERSION}.tgz \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-credentials
```

#### Step 4: Verify Installation

Check that the CSI driver pods are running:

```bash
# Check controller pod
kubectl get pods -n kube-system -l app.kubernetes.io/component=controller

# Check node pods (one per node)
kubectl get pods -n kube-system -l app.kubernetes.io/component=node

# Verify storage class was created
kubectl get storageclass evroc-standard
```

See the [Helm chart README](chart/README.md) for detailed configuration options and the [Configuration Guide](docs/CONFIGURATION.md) for advanced configuration.

### Deployment (Development)

For development and testing:

1. **Build and push Docker image:**
   ```bash
   make docker
   make push
   ```

   Or set a custom registry:
   ```bash
   DOCKER_REGISTRY=your-registry.com/your-org make docker push
   ```

2. **Install to Kubernetes:**
   ```bash
   make install
   ```

3. **View logs:**
   ```bash
   make logs
   ```

4. **Uninstall:**
   ```bash
   make uninstall
   ```

## Project Structure

```
.
├── cmd/
│   └── evroc-csi-driver/      # Main CSI driver entry point
├── pkg/
│   ├── auth/                  # OIDC/OAuth2 authentication
│   ├── common/                # Common validation helpers
│   ├── config/                # Configuration loading and validation
│   ├── controller/            # CSI Controller Service
│   ├── driver/                # Driver orchestration
│   ├── evroc/                 # evroc REST API client and storage backend
│   ├── filesystem/            # Filesystem operations (format, mount, etc.)
│   ├── identity/              # CSI Identity Service
│   ├── metrics/               # Prometheus metrics
│   ├── node/                  # CSI Node Service
│   └── version/               # Version information
├── chart/                     # Helm chart for deployment
│   ├── dashboards/            # Grafana dashboard configurations
│   └── templates/             # Helm templates
├── deploy/
│   └── kubernetes/            # Kubernetes manifests
│       ├── csidriver.yaml     # CSIDriver object
│       ├── rbac.yaml          # RBAC permissions
│       ├── daemonset.yaml     # Node service DaemonSet
│       ├── controller.yaml    # Controller service Deployment
│       ├── storageclass.yaml  # StorageClass definition
│       ├── metrics-service.yaml # Metrics service for Prometheus
│       ├── example-pvc.yaml   # Example PersistentVolumeClaim
│       ├── example-pod.yaml   # Example Pod using PVC
│       └── examples/
│           └── config.yaml    # Example driver configuration
├── test/
│   ├── mocks/                 # Mock implementations for testing
│   ├── sanity/                # CSI sanity tests
│   └── benchmarks/            # Performance benchmarks
├── docs/                      # Architecture and design documentation
├── CHANGELOG.md               # Version history and release notes
├── CONTRIBUTING.md            # Contribution guidelines
├── Dockerfile                 # Multi-stage Docker build
├── Makefile                   # Build and deployment tasks
└── README.md                  # This file
```

## Configuration

### Command-Line Flags

- `--endpoint` - CSI endpoint Unix socket path (default: `/tmp/csi.sock`)
- `--node-id` - Node ID for CSI operations (default: hostname)
- `--mode` - Driver mode: `controller`, `node`, or `all` (default: `all`)

### Configuration File

The driver requires a YAML configuration file mounted at `/etc/evroc-csi/config.yaml`. See `deploy/kubernetes/examples/config.yaml` for a complete example.

**Minimal required configuration:**
```yaml
evroc:
  organization: <orgId>
  project: <projectId>

auth:
  username: my-username         # OIDC username
  password: my-password         # OIDC password
```

**Optional fields with defaults:**
- `evroc.restURL` - REST API endpoint (defaults to `https://api.cloud.evroc.com`)
- `auth.issuerURL` - OIDC issuer (defaults to `https://authn.iam.evroc.com/realms/evroc-customer`)
- `auth.clientID` - OAuth2 client ID (defaults to `csi-driver`)
- `infrastructure.region` - Cloud region (e.g., `se-sto`)
- `csi.identifier` - Unique driver instance ID (optional)

## Features and Limitations

### Supported Features

**Volume Operations:**
- ✅ Volume creation and deletion via evroc Disk resources
- ✅ Volume attach/detach via HotSwapDiskAttachment resources
- ✅ Volume staging with ext4 filesystem formatting
- ✅ Volume publishing to pods (mount and bind mount)
- ✅ Volume statistics reporting

**Access Modes:**
- ✅ ReadWriteOnce (RWO) - Single node read-write
- ✅ ReadWriteOncePod (RWOPod) - Single pod read-write
- ✅ ReadOnlyMany (ROX) - Multiple nodes read-only (filesystem volumes only)

**Volume Types:**
- ✅ Filesystem volumes (ext4)
- ✅ Block volumes (raw block devices)

**Additional Features:**
- ✅ Topology-aware scheduling (zone-based)
- ✅ OIDC authentication with automatic token refresh
- ✅ Idempotent operations

### Current Limitations

**Filesystem Support:**
- ❌ Only ext4 filesystem is currently supported
- ❌ Other filesystems (xfs, btrfs, etc.) are not implemented

**Volume Features:**
- ❌ Volume snapshots - Not implemented
- ❌ Volume cloning - Not implemented
- ❌ Volume expansion - Not implemented

**Access Modes:**
- ❌ ReadOnlyMany (ROX) is not supported for block volumes

## Development

### Testing

Run tests with:
```bash
make test
```

Run with coverage:
```bash
go test -v -race -coverprofile=coverage.txt ./...
```

### Code Quality

Format code:
```bash
make fmt
```

Run static analysis:
```bash
make vet
```

Run all checks:
```bash
make verify
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the driver binary |
| `make run` | Run the driver locally |
| `make test` | Run tests |
| `make lint` | Run linters |
| `make clean` | Clean build artifacts |
| `make docker` | Build Docker image |
| `make push` | Push Docker image to registry |
| `make generate` | Run code generation (placeholder) |
| `make manifest` | View Kubernetes manifests |
| `make install` | Install to Kubernetes cluster |
| `make uninstall` | Uninstall from Kubernetes cluster |
| `make logs` | View driver logs from Kubernetes |
| `make verify` | Run all verification checks |

## Kubernetes Manifests

The driver is deployed as a DaemonSet that runs on all nodes. It includes:

- **CSIDriver object** - Declares the driver to Kubernetes
- **ServiceAccount, ClusterRole, ClusterRoleBinding** - RBAC permissions
- **DaemonSet** - Runs driver on all nodes with:
  - Main driver container
  - CSI node-driver-registrar sidecar

## Security

### Image and Chart Signing

All Docker images and Helm charts are signed with [Cosign](https://github.com/sigstore/cosign) using keyless signing via GitHub OIDC. This ensures the authenticity and integrity of released artifacts.

#### Verify Docker Image Signature

Install Cosign and verify the image signature:

```bash
# Install Cosign (if not already installed)
# See: https://docs.sigstore.dev/cosign/installation/

# Verify image signature (keyless)
cosign verify \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:v0.1.0
```

#### Verify Helm Chart Signature

```bash
# Verify Helm chart signature (keyless)
cosign verify \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  oci://ghcr.io/evroc-oss/evroc-csi-driver:0.1.0
```

#### Verify SBOM Attestation

```bash
# Verify and view SBOM attestation
cosign verify-attestation \
  --type spdxjson \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:v0.1.0
```

#### Verify SLSA Provenance

```bash
# Verify SLSA provenance attestation
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:v0.1.0
```

### Supply Chain Security

- **Signed Images**: All images and charts are cryptographically signed with Cosign
- **SBOM**: Software Bill of Materials (SPDX format) attached to each release
- **SLSA Provenance**: Build provenance attestations for supply chain transparency
- **Pinned Actions**: GitHub Actions are pinned to commit SHAs to prevent supply chain attacks

## License

Licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

## Support

This repository is a public release mirror. Development happens internally.

For issues and questions:
- **Issues**: Raise issues through [evroc support channels](https://docs.evroc.com/support.html).
- **Documentation**: See the [docs](docs/) directory for architecture and development guides
