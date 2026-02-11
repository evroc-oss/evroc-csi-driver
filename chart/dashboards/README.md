# evroc CSI Driver Grafana Dashboard

This directory contains pre-built Grafana dashboards for monitoring the evroc CSI driver.

## Dashboard Overview

The `evroc-csi-driver.json` dashboard provides comprehensive monitoring of:

### Volume Operations
- **Volume Operations Rate**: Real-time rate of volume create/delete/attach/detach operations
- **Volume Operation Duration**: P50, P95, and P99 latency percentiles for all volume operations
- **Volume Operation Errors**: Error rates broken down by operation and error type
- **Volumes by State**: Current count of volumes in different states

### Node Operations
- **Node Operations Rate**: Real-time rate of node-level operations (stage/unstage/publish/unpublish)
- **Node Operation Duration**: P50, P95, and P99 latency percentiles for mount/unmount operations
- **Node Operation Errors**: Error rates for node operations by error type
- **Attachments by State**: Current count of volume attachments in different states

### API Calls
- **API Calls Rate**: Rate of REST API calls by method and status
- **API Call Duration**: P50, P95, and P99 latency percentiles for API calls
- **API Call Errors**: Error rates for API calls by method and error type

### System Metrics
- **Goroutines**: Number of active goroutines per pod
- **Memory Usage**: Heap allocation and usage per pod

## Installation Methods

### Method 1: Import via Grafana UI

1. Enable metrics in your Helm installation:
   ```bash
   helm upgrade --install evroc-csi-driver ./chart \
     --set metrics.enabled=true \
     --set metrics.port=9090
   ```

2. Access your Grafana instance and navigate to **Dashboards** → **Import**

3. Upload the `evroc-csi-driver.json` file or paste its contents

4. Select your Prometheus datasource when prompted

5. Click **Import**

### Method 2: ConfigMap-based (GitOps)

If you're using the Grafana Operator or automatic dashboard provisioning:

1. Create a ConfigMap with the dashboard:
   ```bash
   kubectl create configmap evroc-csi-dashboard \
     --from-file=evroc-csi-driver.json \
     -n monitoring
   ```

2. Label the ConfigMap so Grafana discovers it:
   ```bash
   kubectl label configmap evroc-csi-dashboard \
     grafana_dashboard=1 \
     -n monitoring
   ```

### Method 3: Helm Chart Integration

Enable ServiceMonitor and dashboard provisioning via Helm:

```yaml
metrics:
  enabled: true
  port: 9090
  serviceMonitor:
    enabled: true
    interval: 30s
    scrapeTimeout: 10s
```

Then install the dashboard via the Grafana sidecar:

```bash
kubectl create configmap evroc-csi-dashboard \
  --from-file=chart/dashboards/evroc-csi-driver.json \
  -n monitoring \
  -o yaml --dry-run=client | \
kubectl label -f - grafana_dashboard=1 --local --dry-run=client -o yaml | \
kubectl apply -f -
```

## Customization

The dashboard uses template variables:
- **datasource**: Select your Prometheus datasource (defaults to "Prometheus")

You can customize:
- Time range (default: last 1 hour)
- Refresh interval (default: 30 seconds)
- Alert thresholds in individual panels
- Query intervals (currently 5m rate calculations)

## Metrics Reference

All metrics are prefixed with `evroc_csi_`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `volume_operations_total` | Counter | operation, status | Total volume operations |
| `volume_operations_duration_seconds` | Histogram | operation | Duration of volume operations |
| `volume_operations_errors_total` | Counter | operation, error_type | Volume operation errors |
| `volumes_total` | Gauge | state | Current volumes by state |
| `volume_size_bytes` | Gauge | volume_id | Size of volumes |
| `node_operations_total` | Counter | operation, status | Total node operations |
| `node_operations_duration_seconds` | Histogram | operation | Duration of node operations |
| `node_operations_errors_total` | Counter | operation, error_type | Node operation errors |
| `attachments_total` | Gauge | state | Current attachments by state |
| `api_calls_total` | Counter | method, status | Total API calls |
| `api_calls_duration_seconds` | Histogram | method | Duration of API calls |
| `api_calls_errors_total` | Counter | method, error_type | API call errors |

## Troubleshooting

### Dashboard shows "No data"

1. Verify metrics are enabled:
   ```bash
   kubectl get svc -l app.kubernetes.io/name=evroc-csi-driver
   ```

2. Check if metrics endpoint is accessible:
   ```bash
   kubectl port-forward svc/evroc-csi-controller 9090:9090
   curl http://localhost:9090/metrics
   ```

3. Verify Prometheus is scraping the targets:
   - Check Prometheus UI → Status → Targets
   - Look for `evroc-csi-driver` targets

4. Verify ServiceMonitor is created (if using Prometheus Operator):
   ```bash
   kubectl get servicemonitor -A | grep evroc
   ```

### Incorrect data or missing series

- Check that both controller and node pods are running
- Verify the job label in Prometheus matches the dashboard queries
- Adjust the time range if you just deployed the driver

## Example Queries

Here are some useful PromQL queries for custom panels:

```promql
# Volume operation success rate
sum(rate(evroc_csi_volume_operations_total{status="success"}[5m])) /
sum(rate(evroc_csi_volume_operations_total[5m])) * 100

# Average volume size
avg(evroc_csi_volume_size_bytes) / (1024*1024*1024)  # in GB

# Top 5 slowest operations
topk(5,
  histogram_quantile(0.99,
    sum(rate(evroc_csi_volume_operations_duration_seconds_bucket[5m]))
    by (operation, le)
  )
)
```

## Dashboard Updates

When updating the dashboard:

1. Export from Grafana UI (share → Export → Save to file)
2. Update `evroc-csi-driver.json` in this directory
3. Commit and push changes
4. Reimport or update the ConfigMap to reflect changes
