<div align="center">
  <img src="docs/images/evroc-logo.png" alt="evroc" width="300"/>
</div>

# evroc CSI Driver

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![golangci-lint](https://img.shields.io/github/actions/workflow/status/evroc-oss/evroc-csi-driver/ci.yml?branch=main&label=golangci-lint&logo=go)](https://github.com/evroc-oss/evroc-csi-driver/actions/workflows/ci.yml)

A Container Storage Interface (CSI) driver for evroc, implementing the [CSI specification](https://github.com/container-storage-interface/spec/blob/master/spec.md) with full storage backend integration using the evroc REST API.

## Table of Contents

- [Overview](#overview)
- [Features](#features)
  - [Current Limitations](#current-limitations)
- [Deploying evroc CSI Driver](#deploying-evroc-csi-driver)
  - [Prerequisites](#prerequisites)
    - [Node Runtime Requirements](#node-runtime-requirements)
      - [Required Packages](#required-packages)
      - [Installation by Distribution](#installation-by-distribution)
  - [Installation](#installation)
    - [Step 1: Label Your Nodes](#step-1-label-your-nodes)
    - [Step 2: Create a service account](#step-2-create-a-service-account)
    - [Step 3: Create Configuration Secret](#step-3-create-configuration-secret)
    - [Step 4: Install via Helm](#step-4-install-via-helm)
    - [Step 5: Verify Installation](#step-5-verify-installation)
- [Usage Example](#usage-example)
  - [Step 1: Create a PersistentVolumeClaim](#step-1-create-a-persistentvolumeclaim)
  - [Step 2: Create a Pod using the PVC](#step-2-create-a-pod-using-the-pvc)
  - [Step 3: Verify the Volume](#step-3-verify-the-volume)
- [License](#license)
- [Support](#support)

## Overview

This CSI driver implements all three CSI services:
- **Identity Service** - Returns plugin information and capabilities
- **Controller Service** - Handles volume lifecycle (create, delete, attach, detach) via evroc Disk and HotSwapDiskAttachment resources
- **Node Service** - Handles volume mounting/unmounting on nodes with ext4 filesystem support

The driver integrates with evroc's REST API to manage persistent storage volumes and their attachments to virtual machines.

## Features

**Volume Operations:**
- ✅ Volume creation and deletion via evroc Disk resources
- ✅ Volume attachment/detachment via HotSwapDiskAttachment resources
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

### Current Limitations

**Filesystem Support:**
- ❌ Other filesystems (xfs, btrfs, etc.) are not implemented

**Volume Features:**
- ❌ Volume snapshots - Not implemented
- ❌ Volume cloning - Not implemented
- ❌ Volume expansion - Not implemented

**Access Modes:**
- ❌ ReadOnlyMany (ROX) is not supported for block volumes

## Deploying evroc CSI Driver

### Prerequisites

- Kubernetes 1.28.0+
- Helm 3.0+ (for Helm installation)
- Kubernetes nodes must be evroc VMs
- You must know what zone (one of 'a', 'b', or 'c') your VMs are in. 

#### Node Runtime Requirements

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

### Installation

#### Step 1: Label Your Nodes

Each Kubernetes node must have a zone label. Set it to one of the valid zones:

```bash
kubectl label node <node-name> topology.kubernetes.io/zone=a
```

Valid zone values: `a`, `b`, or `c`

You should run e.g. `evroc compute vm get <vm name>` to determine what zone a VM is in

#### Step 2: Create a service account

The CSI driver needs a service account with permissions to manage disks and attach them to VMs. Create one using the [evroc CLI](https://docs.evroc.com/cli.html):

**1) Create the service account:**

```bash
evroc iam serviceaccount create csi-driver
```

**2) Create a credential for the service account:**

```bash
evroc iam serviceaccount credential create csi-driver-key --service-account csi-driver
```

Save the private key output — it is only shown once. This is the `service_account_secret` value for your config.

**3) Assign required roles:**

The service account needs permissions to manage disks and disk attachments to VMs:

```bash
SA_PRINCIPAL="/iam/projects/<your-project-id>/serviceAccounts/csi-driver"

evroc iam rolebinding assign --principal "$SA_PRINCIPAL" --role /iam/roles/kubernetesCSIAgent
```

#### Step 3: Create Configuration Secret

Create a configuration file using the credentials from Step 2:

```bash
cat > /tmp/evroc-csi-config.yaml <<EOF
auth:
  service_account_id: "csi-driver"
  service_account_secret: "your-jwk-private-key-from-credential-create"

context:
  organization: "your-organization-id"
  project: "your-project-id"
  region: "se-sto"
EOF

kubectl create secret generic evroc-credentials \
  --namespace=kube-system \
  --from-file=config.yaml=/tmp/evroc-csi-config.yaml

rm /tmp/evroc-csi-config.yaml
```

#### Step 4: Install via Helm

Install the CSI driver using Helm:

```bash
helm install evroc-csi-driver \
  https://github.com/evroc-oss/evroc-csi-driver/releases/download/v0.1.6/evroc-csi-driver-0.1.6.tgz \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-credentials
```

You may want to verify the supply chain security of this artefact before deploying, if so, follow [this document](SUPPLY_CHAIN_SECURITY.md).

#### Step 5: Verify Installation

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

## Usage Example

Once the evroc CSI driver is installed, you can create a `PersistentVolumeClaim` and mount it into a pod using the `evroc-standard` StorageClass.

### Step 1: Create a PersistentVolumeClaim

Apply the [example PVC](deploy/kubernetes/examples/example-pvc.yaml):

```bash
kubectl apply -f https://raw.githubusercontent.com/evroc-oss/evroc-csi-driver/main/deploy/kubernetes/examples/example-pvc.yaml
```

### Step 2: Create a Pod using the PVC

Apply the [example Pod](deploy/kubernetes/examples/example-pod.yaml):

```bash
kubectl apply -f https://raw.githubusercontent.com/evroc-oss/evroc-csi-driver/main/deploy/kubernetes/examples/example-pod.yaml
```

### Step 3: Verify the Volume

```bash
# Check that the PVC is bound
kubectl get pvc evroc-test-pvc

# Check that the pod is running
kubectl get pod evroc-test-pod

# Read the test file written to the volume
kubectl logs evroc-test-pod
```

## License

Licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

## Support

This repository is a public release mirror. Development happens internally.

For issues and questions:
- **Issues**: Raise issues through [evroc support channels](https://docs.evroc.com/support.html).
- **Documentation**: See the [docs](docs/) directory for architecture and development guides
