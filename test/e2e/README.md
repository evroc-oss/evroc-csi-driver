# E2E Tests for EVROC CSI Driver

End-to-end tests for the EVROC CSI driver using Ansible for cluster setup and Go tests for validation.

## Prerequisites

- Go 1.21+
- Ansible 2.9+
- kubectl
- Test VM with:
  - SSH access (port 22) with sudo privileges
  - **TCP port 6443 accessible** from your machine (for kubectl/k3s API access)
- evroc API credentials (username/password)

## Setup

### 1. Configure Test VM

```bash
cp test/e2e/ansible/inventory/e2e-vms.yaml.example test/e2e/ansible/inventory/e2e-vms.yaml
```

Edit `e2e-vms.yaml` with your VM IP, SSH user, SSH key path, and zone.

**Network Requirements:**
- Port 22 (SSH) - for Ansible to configure the VM
- Port 6443 (k3s API) - for kubectl and test framework to access the cluster

### 2. Configure CSI Driver

```bash
cp test/e2e/config/e2e-csi-config.yaml.example test/e2e/config/e2e-csi-config.yaml
```

Edit `e2e-csi-config.yaml` with your organization ID, project ID, and credentials.

**Note**: `test/e2e/config/e2e-csi-config.yaml` and `test/e2e/ansible/inventory/e2e-vms.yaml` are gitignored.

## Run Tests

### Option 1: Local Build (Default)

Builds the CSI driver from source and deploys to the test cluster:

```bash
./test/e2e/run-e2e.sh
```

### Option 2: Deploy from GHCR

Uses a pre-built image from GitHub Container Registry:

```bash
# Run tests with GHCR image (latest)
./test/e2e/run-e2e-ghcr.sh

# Or specify a specific tag
GHCR_IMAGE_TAG=v0.1.0-rc1 ./test/e2e/run-e2e-ghcr.sh
```

Both options will:
1. Deploy K3s on the test VM
2. Install the CSI driver
3. Run all E2E tests

## Test Structure

- `basic/` - Core CSI driver tests (10 tests covering volume lifecycle, topology, persistence)
- `ansible/playbooks/` - Infrastructure setup playbooks
- `framework/` - Test utilities and helpers

## Monitoring with Grafana

View live CSI driver metrics during testing:

### 1. Deploy Grafana and Prometheus

```bash
# Deploy monitoring stack (Prometheus + Grafana + Dashboards)
./test/e2e/deploy-dashboards.sh
```

This deploys:
- Prometheus (scrapes CSI driver metrics)
- Grafana (visualization)
- Two dashboards: Operational and Developer

### 2. Access Grafana

**Option A: Via kubectl port-forward** (requires k3s API access on port 6443)

```bash
# Port-forward Grafana (in one terminal)
export KUBECONFIG=$HOME/.kube/e2e-k3s-config
kubectl port-forward -n default svc/grafana 3000:3000
```

**Option B: Via SSH tunnel** (only requires SSH access, no port 6443 needed)

```bash
# SSH tunnel forwarding Grafana's NodePort 30030 to localhost:3000
ssh -L 3000:localhost:30030 evroc-user@<VM_IP>

# Leave the SSH session open while viewing dashboards
```

Both options expose Grafana at http://localhost:3000:
- Username: `admin`
- Password: `admin`

**When to use SSH tunnel:**
- Port 6443 is blocked/firewalled
- You only have SSH access to the VM
- More secure (doesn't require k3s API exposure)

### 3. View Dashboards

- **Developer Dashboard**: Detailed metrics for debugging and optimization
  - API call rates and latencies
  - Volume operation performance
  - Resource usage (CPU, memory, goroutines)
  - http://localhost:3000/d/evroc-csi-developer

- **Operational Dashboard**: High-level health monitoring
  - Success rates and error rates
  - Active volumes
  - SLA compliance tracking
  - http://localhost:3000/d/evroc-csi-operational

See `dashboards/README.md` for detailed dashboard documentation.

## Manual Operations

### Deploy CSI Driver Manually

```bash
cd test/e2e/ansible

# Local build:
ansible-playbook -i inventory/e2e-vms.yaml playbooks/deploy-csi-driver.yaml

# From GHCR:
ansible-playbook -i inventory/e2e-vms.yaml playbooks/deploy-csi-driver-ghcr.yaml \
  -e ghcr_image_tag=v0.1.0-rc1
```

### Run Specific Tests

```bash
# Local build:
./test/e2e/run-e2e.sh TestVolumeDataPersistence

# Or from GHCR:
GHCR_IMAGE_TAG=v0.1.0-rc1 ./test/e2e/run-e2e-ghcr.sh TestVolumeDataPersistence

# Or manually:
export KUBECONFIG=$HOME/.kube/e2e-k3s-config
cd test/e2e/basic
go test -v -run TestVolumeDataPersistence
```

### Cleanup Cluster

```bash
# Using either script:
./test/e2e/run-e2e.sh --cleanup
# or
./test/e2e/run-e2e-ghcr.sh --cleanup

# Or manually:
ansible-playbook -i test/e2e/ansible/inventory/e2e-vms.yaml test/e2e/ansible/playbooks/uninstall-k3s.yaml
```

## Troubleshooting

**SSH fails**: Verify VM IP and key in `e2e-vms.yaml`

**kubectl connection fails**: Ensure port 6443 is accessible from your machine to the VM:
```bash
nc -zv <VM_IP> 6443
# or
telnet <VM_IP> 6443
```

**Auth fails**: Check credentials in `e2e-csi-config.yaml`, test with:
```bash
make rest-tool
./bin/rest-tool --config test/e2e/config/e2e-csi-config.yaml list-volumes
```

**Zone mismatch**: Update `topologyZone` in `test/e2e/basic/main_test.go` to match your VM's zone
