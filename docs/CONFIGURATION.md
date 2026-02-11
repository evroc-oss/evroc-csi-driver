# CSI Driver Configuration

## Overview

The evroc CSI driver is configured via a Kubernetes Secret containing a YAML configuration file.

**Prerequisites:**
- Kubernetes nodes must be evroc VMs (the CSI driver attaches evroc disks to nodes via the evroc REST API)
- Nodes require `blkid` and `mkfs.ext4` utilities (see README for installation)

**Controller pod**: Requires the Secret mounted at `/etc/evroc-csi/config.yaml` and the `--config` flag. Reads API credentials and configuration parameters.

**Node pods**: Run without configuration by default. Optionally, mount the Secret and add the `--config` flag to read configuration parameters (credentials are ignored by the node).

## Configuration File Format

The configuration is provided as a YAML file with the following structure:

```yaml
# evroc platform configuration
evroc:
  restURL: https://api.cloud.evroc.com                      # Optional
  organization: <orgId>                                     # Required
  project: <projectId>                                      # Required

# Authentication configuration
auth:
  issuerURL: https://authn.iam.evroc.com/realms/evroc-customer  # Optional
  clientID: csi-driver                              # Optional
  username: service-account@evroc.com  # Required
  password: my-password                             # Required

# Infrastructure configuration
infrastructure:
  region: se-sto

# CSI driver configuration
csi:
  identifier: my-cluster-csi                        # Optional (required for multi-cluster)
```

### Configuration Fields

#### evroc Section (required)

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `restURL` | No | `https://api.cloud.evroc.com` | evroc REST API server URL |
| `organization` | Yes | - | Organization identifier in evroc |
| `project` | Yes | - | Project identifier where VMs and disks are created |

#### Auth Section (required)

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `issuerURL` | No | `https://authn.iam.evroc.com/realms/evroc-customer` | OIDC issuer URL for authentication |
| `clientID` | No | `csi-driver` | OAuth2 client identifier |
| `username` | Yes | - | Username for authentication (service account email) |
| `password` | Yes | - | Password for authentication |

#### Infrastructure Section (optional)

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `region` | Yes (for REST) | - | Cloud region (required for REST API, e.g., `se-sto` for Stockholm) |

#### CSI Section (optional)

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `identifier` | No | - | Unique identifier for this CSI driver instance. **Required when running multiple clusters in the same project** |

## Multi-Cluster Support

Multiple Kubernetes clusters can share the same project. Each cluster must have a unique CSI identifier to prevent conflicts. The driver sets the `managed-by` label on Disk and HotswapDiskAttachment resources in the evroc REST API to track ownership.

### Key Concepts

1. **Project**: A namespace in REST API where all resources (VMs, Disks) are created
   - Multiple K8s clusters can share the same project
   - All CSI drivers point to the same project

2. **CSI Identifier**: Uniquely identifies which K8s cluster created a Disk
   - Critical for multi-cluster setups - prevents clusters from interfering with each other
   - Must be configured when running multiple clusters in the same project
   - Set as a label on Disk/HotswapDiskAttachment resources in evroc API: `managed-by: <csi-identifier>`
   - Prevents one cluster from deleting another cluster's disks

3. **Zone (Topology)**: Determines where disks are created
   - Zone information comes from Kubernetes node labels (`topology.kubernetes.io/zone`) on your cluster
   - These labels must be applied manually to your nodes (see Multi-Zone Deployments section)
   - Users are in charge of topology labelling the nodes (evroc does not provide a Cloud Controller Manager)
   - Disks are automatically created in the same zone as the node where the pod is scheduled
   - The CSI controller receives zone information through Kubernetes topology requirements (no manual zone configuration needed)

## Deployment Setup

The CSI driver can be deployed using either kubectl or Helm. Both methods require:
1. Labeling nodes with topology zone information
2. Creating a Kubernetes Secret with the configuration

### Prerequisites: Node Labeling

**CRITICAL:** Before deploying the CSI driver, all Kubernetes nodes must have the `topology.kubernetes.io/zone` label set. The CSI driver validates this on startup.

Label each node with one of the valid zones:

```bash
kubectl label node <node-name> topology.kubernetes.io/zone=a
```

Valid zone values: `a`, `b`, or `c`

These zones correspond to the evroc platform availability zones in your region. Without this label, the node pods will crash with:
```
node <name> missing topology.kubernetes.io/zone label - zone information is required for multi-zone deployments
```

To label all nodes at once:

```bash
# Label all nodes with zone 'a' (adjust as needed for your topology)
kubectl get nodes -o name | xargs -I {} kubectl label {} topology.kubernetes.io/zone=a --overwrite
```

### Create Configuration Secret

Create a YAML configuration file:

**Minimal configuration (production defaults):**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: evroc-csi-config
  namespace: kube-system
type: Opaque
stringData:
  config.yaml: |
    evroc:
      organization: <orgId>
      project: <projectId>

    auth:
      username: service-account@evroc.com
      password: my-password

    infrastructure:
      region: se-sto
```

Apply the Secret:

```bash
kubectl apply -f evroc-csi-config.yaml
```

### Deployment with Helm

Install the driver using Helm, referencing the Secret you created:

```bash
helm install evroc-csi-driver ./chart \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-csi-config
```

The Helm chart automatically mounts the Secret into the controller pod. See `chart/values.yaml` for additional configuration options (resource limits, tolerations, etc.).

### Deployment with kubectl

Deploy the CSI driver using the provided Kubernetes manifests:

```bash
# 1. Create the configuration Secret (required for controller)
kubectl apply -f evroc-csi-config.yaml

# 2. Deploy the CSI driver components
kubectl apply -f deploy/kubernetes/rbac.yaml
kubectl apply -f deploy/kubernetes/csidriver.yaml
kubectl apply -f deploy/kubernetes/storageclass.yaml
kubectl apply -f deploy/kubernetes/controller.yaml
kubectl apply -f deploy/kubernetes/daemonset.yaml
kubectl apply -f deploy/kubernetes/metrics-service.yaml  # optional
```

**What gets deployed:**
- **Controller Deployment**: Handles volume provisioning, attachment, and deletion via REST API
- **Node DaemonSet**: Handles volume staging and mounting on each node
- **RBAC**: ServiceAccount, ClusterRole, and ClusterRoleBinding for CSI operations
- **CSIDriver**: Registers the driver with Kubernetes
- **StorageClass**: Default storage class for dynamic provisioning
- **Metrics Service**: Exposes Prometheus metrics (optional)

**Configuration requirements:**
- The **controller** requires the configuration Secret (for REST API credentials)
- The **node DaemonSet** runs without configuration by default (optional parameters like `csi.maxVolumesPerNode` can be enabled by mounting the Secret)

#### Verify Deployment

```bash
# Check controller is running
kubectl get deployment -n kube-system evroc-csi-controller

# Check node pods are running on all nodes
kubectl get daemonset -n kube-system evroc-csi-node

# Check CSI driver is registered
kubectl get csidrivers csi.evroc.com
```

#### How the Configuration is Mounted

##### Controller Deployment

The controller Deployment requires the configuration Secret:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: evroc-csi-controller
  namespace: kube-system
spec:
  template:
    spec:
      containers:
        - name: evroc-csi-driver
          image: evroc-csi-driver:latest
          args:
            - --mode=controller
            - --config=/etc/evroc-csi/config.yaml
          volumeMounts:
            - name: config
              mountPath: /etc/evroc-csi
              readOnly: true
      volumes:
        - name: config
          secret:
            secretName: evroc-csi-config
```

##### Node DaemonSet

The node DaemonSet must be deployed on all nodes but does not require configuration:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: evroc-csi-node
  namespace: kube-system
spec:
  template:
    spec:
      hostNetwork: true  # Uses host's hostname as node ID
      containers:
        - name: evroc-csi-driver
          image: evroc-csi-driver:latest
          args:
            - --mode=node
          # No --node-id needed! Defaults to hostname (which is the node name)
```

**Security Note:** This separation ensures that:
- evroc credentials are only on controller pods (typically on control plane)
- Node pods (running on every worker) have no access to credentials
- Minimizes attack surface if a worker node is compromised

### Verify Configuration

After deploying, check that configuration loaded correctly:

```bash
# Check controller logs
kubectl logs -n kube-system -l app=evroc-csi-controller -c evroc-csi-driver

# Should show: "Loaded config: Config{...}"

# Verify configuration Secret is mounted
kubectl exec -n kube-system <pod-name> -- ls -la /etc/evroc-csi/
# Should show: config.yaml

# Check if config file exists (but don't read it - contains credentials!)
kubectl exec -n kube-system <pod-name> -- test -f /etc/evroc-csi/config.yaml && echo "Config file exists"
```

## Troubleshooting

### Configuration not loading

```bash
# Check if Secret exists
kubectl get secret evroc-csi-config -n kube-system

# Check if file is mounted
kubectl exec -n kube-system <pod> -- ls -la /etc/evroc-csi/

# Check logs for configuration errors
kubectl logs -n kube-system <pod> -c evroc-csi-driver | grep -i "config\|error"
```

### Invalid configuration

Check logs for specific validation error messages:

```bash
kubectl logs -n kube-system <pod> -c evroc-csi-driver | grep -i error
```

Common validation errors:
- `auth.username is required` - Missing username in auth section
- `auth.password is required` - Missing password in auth section
- `evroc.organization is required` - Missing organization field
- `evroc.project is required` - Missing project field
- `infrastructure.region is required` - Region field is missing

### CSI Identifier Issues

If you see warnings about CSI identifier or experience issues with multiple clusters:

```bash
# Check CSI identifier configuration
kubectl logs -n kube-system <pod> -c evroc-csi-driver | grep -i identifier

# Update Secret to add identifier (required for multi-cluster setups)
kubectl edit secret evroc-csi-config -n kube-system
# Add csi.identifier field to the config.yaml
```

## Security Notes

- All configuration including credentials is stored in a Kubernetes Secret
- Configuration is mounted as a read-only volume, never passed as environment variables
- Credentials are never logged or exposed in error messages (shown as `<redacted>` in logs)
- Pods only have access to explicitly mounted Secrets
- No RBAC permissions needed to read Secrets (uses volume mounts)

## Single-Zone Deployments

**Zone labels are required** - all nodes must be labeled with their zone, even in single-zone deployments.

Label all nodes with the same zone (zone values correspond to evroc availability zones: a, b, or c):

```bash
# Label all nodes with zone A (evroc availability zone)
kubectl label nodes --all topology.kubernetes.io/zone=a
```

**Why zone labels are required:**
- The node plugin will not start without zone labels
- The CSI driver requires zone information for all volume operations
- Ensures volumes are created in the correct availability zone
- Enables future multi-zone expansion without reconfiguration
- Consistent with Kubernetes topology conventions

## Multi-Zone Deployments

The evroc CSI driver supports multi-zone Kubernetes clusters through topology-aware volume provisioning. This requires manual node labeling since evroc does not currently provide a Cloud Controller Manager (CCM).

### Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│ Multi-Zone Kubernetes Cluster                           │
│                                                         │
│ Zone A Nodes:                                           │
│  ├─ node-1 (labeled: topology.kubernetes.io/zone=a)     │
│  └─ node-2 (labeled: topology.kubernetes.io/zone=a)     │
│                                                         │
│ Zone B Nodes:                                           │
│  ├─ node-3 (labeled: topology.kubernetes.io/zone=b)     │
│  └─ node-4 (labeled: topology.kubernetes.io/zone=b)     │
│                                                         │
│ CSI Driver Components:                                  │
│  ├─ Controller (reads zone from topology requirements)  │
│  └─ Node DaemonSet (runs on all nodes, no zone config)  │
│                                                         │
│ Volume Creation Flow:                                   │
│  1. Pod scheduled to node-1 (zone A)                    │
│  2. Provisioner reads node-1's zone label               │
│  3. CreateVolume called with zone=a                     │
│  4. Controller creates disk in zone A via REST API      │
└─────────────────────────────────────────────────────────┘
```

### Prerequisites

1. **Node Labeling**: Nodes must be manually labeled with their zone
2. **StorageClass**: Must use `volumeBindingMode: WaitForFirstConsumer` for topology-aware provisioning
3. **Single CSI Config**: One configuration works across all zones

Note: The StorageClass `volumeBindingMode` is configurable in Helm (`storageClass.volumeBindingMode`), but changing it from `WaitForFirstConsumer` will disable zone-aware volume placement.

### Step 1: Label Nodes by Zone

Label each node with its evroc availability zone. evroc currently provides three zones: **a**, **b**, and **c**.

```bash
# Zone A nodes (evroc availability zone a)
kubectl label nodes node-1 topology.kubernetes.io/zone=a
kubectl label nodes node-2 topology.kubernetes.io/zone=a

# Zone B nodes (evroc availability zone b)
kubectl label nodes node-3 topology.kubernetes.io/zone=b
kubectl label nodes node-4 topology.kubernetes.io/zone=b

# Zone C nodes (evroc availability zone c)
kubectl label nodes node-5 topology.kubernetes.io/zone=c
kubectl label nodes node-6 topology.kubernetes.io/zone=c
```

Verify labels:

```bash
kubectl get nodes --show-labels | grep topology.kubernetes.io/zone
```

### Step 2: Create CSI Configuration (No Zone Required)

The CSI configuration **does not need a zone field** because the controller receives zone information from Kubernetes topology requirements:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: evroc-csi-config
  namespace: kube-system
type: Opaque
stringData:
  config.yaml: |
    evroc:
      organization: <orgId>
      project: <projectId>

    auth:
      username: service-account@evroc.com
      password: my-password

    infrastructure:
      region: se-sto
```

### Step 3: Deploy CSI Driver

Deploy the CSI driver with a single configuration that works across all zones:

```bash
# Create Secret
kubectl apply -f evroc-csi-config.yaml

# Deploy CSI driver (controller + node DaemonSet)
kubectl apply -f deploy/kubernetes/
```

The node DaemonSet runs on all nodes regardless of zone - it does not need zone configuration.

### Step 4: Create Topology-Aware StorageClass

The StorageClass **must** use `WaitForFirstConsumer` binding mode:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: evroc-storage
provisioner: disk.csi.evroc.com
volumeBindingMode: WaitForFirstConsumer  # Required for topology
parameters:
  storageClass: persistent
```

**Why `WaitForFirstConsumer`?**
- Volume creation is delayed until a pod is scheduled
- Kubernetes knows which node (and zone) the pod will run on
- CSI provisioner includes zone in `AccessibilityRequirements`
- Controller creates disk in the correct zone

### Volume Creation Flow

1. **User creates PVC:**
   ```yaml
   apiVersion: v1
   kind: PersistentVolumeClaim
   metadata:
     name: my-pvc
   spec:
     storageClassName: evroc-storage
     accessModes: [ReadWriteOncePod]
     resources:
       requests:
         storage: 10Gi
   ```

2. **User creates Pod using PVC:**
   ```yaml
   apiVersion: v1
   kind: Pod
   metadata:
     name: my-pod
   spec:
     containers:
     - name: app
       image: nginx
       volumeMounts:
       - name: data
         mountPath: /data
     volumes:
     - name: data
       persistentVolumeClaim:
         claimName: my-pvc
   ```

3. **Kubernetes schedules Pod to node-1 (zone A)**

4. **CSI provisioner reads node-1's zone label** (`topology.kubernetes.io/zone=a`)

5. **Provisioner calls CreateVolume** with `AccessibilityRequirements{zone: a}`

6. **Controller receives zone from request** and creates disk in zone A via REST API

7. **Volume is attached and mounted** on node-1

### Troubleshooting Multi-Zone

**Volume stuck in Pending:**

```bash
# Check if nodes have zone labels
kubectl get nodes --show-labels | grep topology.kubernetes.io/zone

# Check StorageClass binding mode
kubectl get sc evroc-storage -o yaml | grep volumeBindingMode
# Should be: WaitForFirstConsumer

# Check PVC events
kubectl describe pvc my-pvc
```

**Volume created in wrong zone:**

```bash
# Verify node zone label
kubectl get node <node-name> -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}'

# Check controller logs for zone information
kubectl logs -n kube-system -l app=evroc-csi-controller | grep zone
```

**Node labels missing:**

The driver requires zone labels to start. If nodes don't have the `topology.kubernetes.io/zone` label, the node plugin will fail with "zone information required". Label all nodes as shown in Step 1 before deploying the driver.

## Metrics and Monitoring

The CSI driver exposes Prometheus metrics on port 9090 (controller) and 9091 (node pods).

### Metrics Endpoint

The controller pod exposes metrics at `:9090/metrics`:

```bash
# Port-forward to access metrics locally
kubectl port-forward -n kube-system deploy/evroc-csi-controller 9090:9090

# View metrics
curl http://localhost:9090/metrics
```

### Metrics Service

The driver includes a Kubernetes Service for Prometheus scraping:

```yaml
# deploy/kubernetes/metrics-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: evroc-csi-metrics
  namespace: kube-system
spec:
  selector:
    app: evroc-csi-controller
  ports:
  - name: metrics
    port: 9090
    targetPort: 9090
```

### Prometheus Configuration

Add the following scrape config to your Prometheus:

```yaml
scrape_configs:
  - job_name: 'evroc-csi-driver'
    kubernetes_sd_configs:
      - role: service
        namespaces:
          names:
            - kube-system
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_name]
        action: keep
        regex: evroc-csi-metrics
```

### Grafana Dashboard

A sample Grafana dashboard is provided in `chart/grafana-dashboard.json`. Import this dashboard to visualize:

- Volume operation success/failure rates
- API call latency and error rates
- Volume and attachment counts
- Operation duration histograms

To import the dashboard:

1. Open Grafana
2. Navigate to Dashboards → Import
3. Upload `chart/grafana-dashboard.json`
4. Select your Prometheus datasource

### Available Metrics

See `docs/ARCHITECTURE.md` under "Observability" for the complete list of available metrics.

## Example Configurations

See `deploy/kubernetes/examples/config.yaml` for a complete example configuration file with all available options and comments.
