# Grafana Dashboards

This directory contains canonical Grafana dashboards for monitoring the evroc CSI driver in all environments (development, testing, and production).

## Available Dashboards

### 1. Production Dashboard (`production.json`)

**Target audience:** Production deployments via Helm

**Purpose:** General-purpose monitoring for production environments

**Use case:**
- Production Helm deployments
- Long-term monitoring
- Basic health checks

---

### 2. Operational Dashboard (`operational.json`)

**Target audience:** System administrators and operators

**Purpose:** High-level health monitoring at a glance

**Metrics:**
- Volume Operations Success Rate (with color-coded thresholds)
- Active Volumes count
- Error Rate (last 5 minutes)
- Volume Operations Rate by operation and status
- Volume Operation Latency (p95)
- API Call Rate by method and status
- API Call Latency (p95)
- Errors by Operation Type

**Use case:**
- Quick health checks
- Production monitoring
- Incident response
- SLA compliance tracking

---

### 3. Developer Dashboard (`developer.json`)

**Target audience:** CSI driver developers and platform engineers

**Purpose:** Detailed internal metrics for debugging and optimization

**Organized in sections:**

#### API Call Metrics
- API Call Rate (requests/sec by method and status)
- API Call Average Duration
- API Call Latency Heatmap
- API Call Latency Percentiles (p50, p90, p95)
- API Errors by Method Type (stacked view)

#### Volume Operations
- Attachment Operations Rate
- Detach Operations Rate
- Attachment Latency (p50, p90, p95)
- Detach Latency (p50, p90, p95)
- Attach Duration Distribution (bucketed: <10s, 10-30s, 30-60s, >60s)

#### Resource Usage
- CPU Usage (all CSI driver nodes)
- Memory Usage (all CSI driver nodes)
- Network I/O (receive/transmit)

#### Runtime Metrics
- Go Goroutines count
- Go Heap Memory (allocated vs in-use)
- Go GC Duration

**Use case:**
- Performance optimization
- Debugging volume operation failures
- Resource utilization analysis
- Finding memory leaks or goroutine leaks
- Understanding API call patterns

---

## Deployment

Dashboards are automatically deployed when running E2E or soak tests.

### Manual deployment:

```bash
# Deploy both dashboards
./test/e2e/deploy-dashboards.sh

# Or with custom kubeconfig
./test/e2e/deploy-dashboards.sh /path/to/kubeconfig
```

### Accessing Dashboards

After deployment, dashboards are available in Grafana:

1. Access Grafana via SSH tunnel:
   ```bash
   ssh -L 3000:localhost:30030 evroc-user@<VM_IP>
   ```

2. Open Grafana in browser: http://localhost:3000
   - Username: `admin`
   - Password: `admin`

3. Find dashboards:
   - **evroc CSI Driver - Operational** (uid: `evroc-csi-operational`)
   - **evroc CSI Driver - Developer** (uid: `evroc-csi-developer`)

---

## Modifying Dashboards

### Option 1: Edit JSON directly

1. Edit `operational.json` or `developer.json`
2. Deploy changes:
   ```bash
   ./test/e2e/deploy-dashboards.sh
   ```
3. Grafana will automatically reload dashboards (updateIntervalSeconds: 10)

### Option 2: Edit in Grafana UI

1. Make changes in Grafana UI
2. Save and export dashboard JSON
3. Copy JSON to appropriate file (`operational.json` or `developer.json`)
4. Commit changes to repository

---

## Dashboard Properties

Both dashboards use:
- **Auto-refresh:** 10 seconds
- **Time range:** Last 1 hour (operational), Last 15 minutes (developer)
- **Timezone:** Browser timezone
- **Datasource:** Prometheus (auto-provisioned)

---

## Prometheus Queries

All dashboard queries use the following evroc CSI metrics:

**Volume Operations:**
- `evroc_csi_volume_operations_total` (counter)
- `evroc_csi_volume_operations_duration_seconds_bucket` (histogram)
- `evroc_csi_volume_operations_errors_total` (counter)
- `evroc_csi_volumes_total` (gauge)

**API Calls:**
- `evroc_csi_api_calls_total` (counter)
- `evroc_csi_api_calls_duration_seconds_bucket` (histogram)
- `evroc_csi_api_calls_errors_total` (counter)

**Go Runtime:**
- `go_goroutines`
- `go_memstats_heap_alloc_bytes`
- `go_memstats_heap_inuse_bytes`
- `go_gc_duration_seconds_sum/count`
- `process_cpu_seconds_total`
- `process_resident_memory_bytes`
- `process_network_receive_bytes_total`
- `process_network_transmit_bytes_total`

---

## Troubleshooting

**Dashboards not appearing:**
- Check ConfigMaps: `kubectl get configmap | grep grafana-dashboard`
- Restart Grafana: `kubectl rollout restart deployment/grafana -n default`
- Check Grafana logs: `kubectl logs -l app=grafana -n default`

**No data in charts:**
- Verify Prometheus is scraping CSI driver: http://localhost:9090/targets
- Check CSI driver metrics endpoint: `kubectl exec -n kube-system <controller-pod> -- curl localhost:9090/metrics`
- Verify scrape config in monitoring.yaml targets kube-system namespace

**Permission denied when deploying:**
- Ensure KUBECONFIG is set correctly
- Check kubectl access: `kubectl get pods`
