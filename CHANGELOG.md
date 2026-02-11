# Changelog

All notable changes to the evroc CSI driver will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.4] - 2026-02-11

### Added
- Version bump script for automated releases

### Changed
- Improved installation documentation with step-by-step prerequisites
- Enhanced Helm chart README with node labeling requirements
- Updated configuration examples to use generic service account format

### Fixed
- Clarified node topology label requirements (zones must be: a, b, or c)

## [0.1.3] - 2026-02-11

### Changed
- Updates to release pipeline and CI/CD configuration

## [0.1.2] - 2026-02-11

### Changed
- Build and release process improvements

## [0.1.1] - 2026-02-11

### Changed
- Initial release pipeline fixes

## [0.1.0] - 2026-01-08

### Added
- Full CSI specification v1.12.0 compliance
- evroc REST API integration (v1alpha2)
  - Disk resource management (create, delete, list)
  - HotswapDiskAttachment resource management (attach, detach)
- OIDC/OAuth2 authentication with automatic token refresh
- ext4 filesystem support with automatic formatting
- Volume lifecycle operations:
  - CreateVolume / DeleteVolume
  - ControllerPublishVolume / ControllerUnpublishVolume
  - NodeStageVolume / NodeUnstageVolume
  - NodePublishVolume / NodeUnpublishVolume
  - NodeGetVolumeStats (real filesystem statistics)
- Supported access modes:
  - ReadWriteOnce (RWO) - Single node read-write
  - ReadWriteOncePod (RWOPod) - Single pod read-write
  - ReadOnlyMany (ROX) - Multiple nodes read-only (filesystem only)
- Topology-aware volume provisioning (zone-based scheduling)
- Idempotent operations with proper error handling
- Comprehensive Prometheus metrics:
  - Volume operations (create, delete, attach, detach)
  - Node operations (stage, publish, stats)
  - API call metrics (duration, errors)
  - Attachment state tracking
- Resource ownership tracking via `managed-by` labels on evroc API resources
- Configurable node attach limits (default: 128 volumes per node)
- Exponential backoff retry for transient API errors

### Supported Volume Types
- Filesystem volumes (ext4 formatted)
- Block volumes (raw block devices)

### Current Limitations

#### Not Implemented
- **Volume snapshots** - CSI snapshot operations not implemented
- **Volume cloning** - CSI clone operations not implemented
- **Volume expansion** - CSI resize operations not implemented

#### Filesystem Limitations
- Only ext4 filesystem supported
- Other filesystems (xfs, btrfs, etc.) not implemented

#### Access Mode Limitations
- ReadOnlyMany (ROX) not supported for block volumes
- ReadWriteMany (RWX) not supported

### Technical Details

**CSI Services:**
- Identity Service - Plugin metadata and capabilities
- Controller Service - Volume lifecycle via REST API
- Node Service - Volume mounting with ext4 formatting

**Authentication:**
- OIDC Resource Owner Password Credentials (ROPC) flow
- Automatic token refresh (50-minute interval)
- Thread-safe token management

**Storage Backend:**
- REST API endpoint: `https://api.cloud.evroc.com`
- OIDC issuer: `https://authn.iam.evroc.com/realms/evroc-customer`
- API version: `compute/v1alpha2`

**Node Requirements:**
- `blkid` (util-linux package)
- `mkfs.ext4` (e2fsprogs package)

### Documentation
- Complete architecture documentation
- Production-accurate sequence diagrams
- Development guide with k3s setup
- Configuration guide with examples

### Dependencies
- Go 1.24+
- Kubernetes 1.26+
- CSI spec v1.12.0
- evroc REST API v1alpha2

---

[0.1.0]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.1.0

[Unreleased]: https://github.com/evroc-oss/evroc-csi-driver/compare/v0.1.4...HEAD
[0.1.4]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.1.4
[0.1.5]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.1.5
