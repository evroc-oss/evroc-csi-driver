# evroc CSI Driver Helm Chart

This Helm chart deploys the evroc CSI driver for Kubernetes, enabling dynamic provisioning of persistent block storage volumes on evroc virtual machines.

## Prerequisites

- Kubernetes 1.28.0+
- Helm 3.0+
- Kubernetes nodes must be evroc VMs
- Nodes must have `topology.kubernetes.io/zone` label set
- evroc platform credentials (username and password)
- Access to evroc REST API

## Installation

### 1. Label Your Nodes

**IMPORTANT:** Each Kubernetes node must have a zone label before installing the CSI driver:

```bash
kubectl label node <node-name> topology.kubernetes.io/zone=a
```

Valid zone values: `a`, `b`, or `c`

The CSI driver validates this label on startup. Without it, the node pods will fail with:
```
node <name> missing topology.kubernetes.io/zone label - zone information is required for multi-zone deployments
```

### 2. Prepare Credentials

Create a secret with your evroc configuration:

```bash
# Create config.yaml file
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

# Clean up the config file for security
rm config.yaml
```

### 3. Install the Chart

Install directly from a GitHub Release:

```bash
# Replace v0.1.6 with the desired version
helm install evroc-csi-driver \
  https://github.com/evroc-oss/evroc-csi-driver/releases/download/v0.1.6/evroc-csi-driver-0.1.6.tgz \
  --namespace kube-system \
  --set evroc.existingConfigSecret="evroc-credentials"
```

**Optional: Verify chart signature before installation**

All Helm charts are signed with Cosign for supply chain security. To verify:

```bash
# Download chart and signature files
VERSION=0.1.6
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz.sig
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz.pem

# Verify the signature (requires Cosign: https://docs.sigstore.dev/cosign/installation/)
cosign verify-blob evroc-csi-driver-${VERSION}.tgz \
  --signature evroc-csi-driver-${VERSION}.tgz.sig \
  --certificate evroc-csi-driver-${VERSION}.tgz.pem \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"

# Install the verified chart
helm install evroc-csi-driver ./evroc-csi-driver-${VERSION}.tgz \
  --namespace kube-system \
  --set evroc.existingConfigSecret="evroc-credentials"
```

Or from a local clone (for development):

```bash
git clone https://github.com/evroc-oss/evroc-csi-driver.git
helm install evroc-csi-driver ./evroc-csi-driver/chart \
  --namespace kube-system \
  --set evroc.existingConfigSecret="evroc-credentials"
```

### 4. Verify Installation

Check that all components are running:

```bash
# Check controller pod (should be 3/3 Ready)
kubectl get pods -n kube-system -l app.kubernetes.io/component=controller

# Check node pods (should be 2/2 Ready, one per node)
kubectl get pods -n kube-system -l app.kubernetes.io/component=node

# Verify storage class
kubectl get storageclass evroc-standard
```

Expected output:
```
NAME             PROVISIONER             RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION   AGE
evroc-standard   disk.csi.evroc.com      Delete          WaitForFirstConsumer   false                  1m
```

## Configuration

### Credentials Configuration

**IMPORTANT:** Always use `existingConfigSecret` to provide credentials. This is the recommended and secure way to configure the CSI driver.

Create a Kubernetes Secret containing your evroc credentials as shown in the installation steps above, then reference it:

```bash
--set evroc.existingConfigSecret="evroc-credentials"
```

### Other Parameters

The following table lists other configurable parameters of the evroc CSI driver chart:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.replicas` | Number of controller replicas | `1` |
| `controller.image.repository` | Controller image repository | `ghcr.io/evroc-oss/evroc-csi-driver` |
| `controller.image.tag` | Controller image tag | `v0.1.6` |
| `node.image.repository` | Node plugin image repository | `ghcr.io/evroc-oss/evroc-csi-driver` |
| `node.image.tag` | Node plugin image tag | `v0.1.6` |
| `storageClass.create` | Create default storage class | `true` |
| `storageClass.name` | Storage class name | `evroc-standard` |
| `storageClass.isDefault` | Set as default storage class | `false` |
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor for Prometheus Operator | `false` |

## Configuration File Format

The CSI driver uses a `config.yaml` file with the following structure:

```yaml
evroc:
  restURL: "https://api.evroc.com"  # optional, this is the default
  organization: "your-org-id"             # required
  project: "your-project-id"              # required

auth:
  issuerURL: "https://authn.iam.evroc.com/realms/evroc-customer"  # optional, this is the default
  clientID: "csi-driver"                  # optional, this is the default
  username: "service-account@evroc.com"  # required
  password: "your-password"               # required

infrastructure:
  region: "se-sto"                        # required for REST API

csi:  # optional
  identifier: "my-cluster"                # optional, required only for multi-cluster setups
```

**Required fields:**
- `evroc.organization` - Your evroc organization ID
- `evroc.project` - Your evroc project ID
- `auth.username` - CSI driver service account username
- `auth.password` - CSI driver service account password
- `infrastructure.region` - evroc region (e.g., `se-sto`)

**Optional fields with defaults:**
- `evroc.restURL` - Defaults to `https://api.evroc.com`
- `auth.issuerURL` - Defaults to `https://authn.iam.evroc.com/realms/evroc-customer`
- `auth.clientID` - Defaults to `csi-driver`
- `csi.identifier` - Only needed when running multiple clusters in the same project

This configuration is loaded from a Kubernetes secret and maps to the configuration structure defined in `pkg/config/config.go`.

## Example Values File

### Using an existing secret (recommended)

```yaml
evroc:
  existingConfigSecret: "evroc-credentials"

controller:
  replicas: 1
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi

node:
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi

storageClass:
  create: true
  name: evroc-standard
  isDefault: false
  parameters:
    diskStorageClass: "persistent"

metrics:
  enabled: true
  serviceMonitor:
    enabled: false
```

## Usage

After installation, you can create PersistentVolumeClaims:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Block
  resources:
    requests:
      storage: 10Gi
  storageClassName: evroc-standard
```

And use them in pods:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  containers:
  - name: app
    image: ubuntu:22.04
    volumeDevices:
    - name: data
      devicePath: /dev/xvda
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: my-pvc
```

## Uninstalling

```bash
helm uninstall evroc-csi-driver --namespace kube-system
```

## Troubleshooting

### Check Controller Logs

```bash
kubectl logs -n kube-system -l app.kubernetes.io/component=controller -c evroc-csi-driver
```

### Check Node Plugin Logs

```bash
kubectl logs -n kube-system -l app.kubernetes.io/component=node -c evroc-csi-driver
```

### Verify CSI Driver Registration

```bash
kubectl get csidriver disk.csi.evroc.com
```

### Check Events

```bash
kubectl get events -n kube-system --sort-by='.lastTimestamp'
```

## Development

To test the chart locally:

```bash
# Lint the chart
helm lint ./chart

# Render templates
helm template evroc-csi ./chart --namespace kube-system

# Dry run install
helm install evroc-csi ./chart --namespace kube-system --dry-run --debug
```

## License

Apache License 2.0
