#!/bin/bash
set -euo pipefail

# Deploy Grafana dashboards as ConfigMaps
# Usage: ./deploy-dashboards.sh [kubeconfig]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DASHBOARDS_DIR="${PROJECT_ROOT}/dashboards"
KUBECONFIG="${1:-${KUBECONFIG:-${HOME}/.kube/e2e-k3s-config}}"

export KUBECONFIG

echo "[INFO] Deploying Grafana dashboards from ${DASHBOARDS_DIR}"

# Create ConfigMap for operational dashboard
if [ -f "${DASHBOARDS_DIR}/operational.json" ]; then
    echo "[INFO] Creating ConfigMap for operational dashboard..."
    kubectl create configmap grafana-dashboard-operational \
        --from-file=operational.json="${DASHBOARDS_DIR}/operational.json" \
        --dry-run=client -o yaml | kubectl apply -f -
fi

# Create ConfigMap for developer dashboard
if [ -f "${DASHBOARDS_DIR}/developer.json" ]; then
    echo "[INFO] Creating ConfigMap for developer dashboard..."
    kubectl create configmap grafana-dashboard-developer \
        --from-file=developer.json="${DASHBOARDS_DIR}/developer.json" \
        --dry-run=client -o yaml | kubectl apply -f -
fi

echo "[SUCCESS] Dashboards deployed successfully"
echo "[INFO] Restarting Grafana to pick up new dashboards..."
kubectl rollout restart deployment/grafana -n default || echo "[WARN] Could not restart Grafana"

echo "[INFO] Done"
