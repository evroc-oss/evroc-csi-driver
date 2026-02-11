#!/bin/bash
set -euo pipefail

# E2E Test Runner using GHCR images
# Usage: GITHUB_USERNAME=user GITHUB_PAT=token ./test/e2e/run-e2e-ghcr.sh [test-name]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Configuration
K3S_KUBECONFIG="${K3S_KUBECONFIG:-${HOME}/.kube/e2e-k3s-config}"
ANSIBLE_DIR="${SCRIPT_DIR}/ansible"
GHCR_IMAGE_TAG="${GHCR_IMAGE_TAG:-latest}"

# Logging
log_info() { echo "[INFO] $*"; }
log_error() { echo "[ERROR] $*" >&2; }
log_success() { echo "[SUCCESS] $*"; }

# Check prerequisites
check_prerequisites() {
    for cmd in ansible-playbook helm kubectl go; do
        if ! command -v "$cmd" &> /dev/null; then
            log_error "$cmd not found. Please install it."
            exit 1
        fi
    done

    # Validate GitHub credentials
    if [ -z "${GITHUB_USERNAME:-}" ] || [ -z "${GITHUB_PAT:-}" ]; then
        log_error "GHCR deployment requires GITHUB_USERNAME and GITHUB_PAT environment variables"
        log_error ""
        log_error "Example:"
        log_error "  export GITHUB_USERNAME=yourname"
        log_error "  export GITHUB_PAT=ghp_xxxxxxxxxxxx"
        log_error "  $0"
        exit 1
    fi
}

# Setup k3s cluster
setup_k3s() {
    log_info "Setting up k3s cluster..."
    cd "${ANSIBLE_DIR}"

    if ! ansible all -i inventory/e2e-vms.yaml -m ping > /dev/null 2>&1; then
        log_error "Cannot connect to E2E VM. Check inventory/e2e-vms.yaml"
        exit 1
    fi

    if ! ansible-playbook -i inventory/e2e-vms.yaml playbooks/setup-k3s.yaml; then
        log_error "k3s setup failed"
        exit 1
    fi

    log_success "k3s cluster ready"
    cd "${PROJECT_ROOT}"
}

# Deploy CSI driver from GHCR
deploy_csi_driver() {
    log_info "Deploying CSI driver from GHCR..."
    log_info "Image: ghcr.io/evroc-oss/evroc-csi-driver:${GHCR_IMAGE_TAG}"

    cd "${ANSIBLE_DIR}"

    if ! ansible-playbook playbooks/deploy-csi-driver-ghcr.yaml -e ghcr_image_tag="${GHCR_IMAGE_TAG}"; then
        log_error "CSI driver deployment failed"
        exit 1
    fi

    log_success "CSI driver deployed from GHCR"
    cd "${PROJECT_ROOT}"
}

# Run e2e tests
run_tests() {
    log_info "Running e2e tests..."

    cd "${SCRIPT_DIR}/basic"

    export KUBECONFIG="${K3S_KUBECONFIG}"
    export EVROC_PROJECT_ID="${EVROC_PROJECT_ID:-csiproject}"
    export EVROC_REGION="${EVROC_REGION:-se-sto}"

    # Run specific test or all tests
    if [ $# -gt 0 ]; then
        log_info "Running specific test: $1"
        if go test -v -timeout 30m -run "$1"; then
            log_success "Test $1 passed!"
            return 0
        else
            log_error "Test $1 failed"
            return 1
        fi
    else
        log_info "Running all tests"
        if go test -v -timeout 30m; then
            log_success "All tests passed!"
            return 0
        else
            log_error "Tests failed"
            return 1
        fi
    fi
}

# Show usage
usage() {
    cat <<EOF
Usage: $0 [options] [test-name]

This script deploys the CSI driver from GHCR (GitHub Container Registry)
instead of building from source.

Required Environment Variables:
  GITHUB_USERNAME  GitHub username for GHCR authentication
  GITHUB_PAT       GitHub Personal Access Token with read:packages scope

Optional Environment Variables:
  GHCR_IMAGE_TAG   GHCR image tag (default: latest)

Options:
  -h, --help       Show this help
  --setup-only     Setup k3s and deploy CSI driver only
  --test-only      Run tests only (cluster must exist)
  --skip-setup     Skip k3s setup, deploy CSI driver and run tests
  --cleanup        Remove k3s cluster

Examples:
  # Full run with specific tag:
  export GITHUB_USERNAME=yourname
  export GITHUB_PAT=ghp_xxxxxxxxxxxx
  GHCR_IMAGE_TAG=v0.1.0-rc1 $0

  # Run specific test:
  GHCR_IMAGE_TAG=v0.1.0-rc1 $0 TestPVCDeletion

  # Tests only (with existing cluster):
  $0 --test-only

  # Use latest tag:
  $0
EOF
}

# Cleanup
cleanup() {
    log_info "Cleaning up k3s cluster..."

    cd "${ANSIBLE_DIR}"

    if ! ansible-playbook -i inventory/e2e-vms.yaml playbooks/uninstall-k3s.yaml; then
        log_error "Cleanup failed"
        exit 1
    fi

    log_success "Cleanup complete"
    cd "${PROJECT_ROOT}"
}

# Main
main() {
    # Parse arguments
    SETUP_ONLY=false
    TEST_ONLY=false
    SKIP_SETUP=false
    DO_CLEANUP=false
    TEST_NAME=""

    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                usage
                exit 0
                ;;
            --setup-only)
                SETUP_ONLY=true
                shift
                ;;
            --test-only)
                TEST_ONLY=true
                shift
                ;;
            --skip-setup)
                SKIP_SETUP=true
                shift
                ;;
            --cleanup)
                DO_CLEANUP=true
                shift
                ;;
            *)
                TEST_NAME="$1"
                shift
                ;;
        esac
    done

    # Execute based on flags
    if [ "$DO_CLEANUP" = true ]; then
        cleanup
        exit 0
    fi

    check_prerequisites

    if [ "$TEST_ONLY" = false ]; then
        if [ "$SKIP_SETUP" = false ]; then
            setup_k3s
        fi
        deploy_csi_driver
    fi

    if [ "$SETUP_ONLY" = false ]; then
        run_tests "$TEST_NAME"
    else
        log_info "Setup complete. Kubeconfig: ${K3S_KUBECONFIG}"
        log_info "Run tests with: $0 --test-only"
    fi
}

main "$@"
