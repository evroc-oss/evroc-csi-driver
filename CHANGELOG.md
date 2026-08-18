# Changelog

All notable changes to the evroc CSI driver will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.3] - 2026-08-11

### Fixed
- Reconcile a single stale disk attachment by removing it before attaching the disk to the node requested by Kubernetes. Multiple attachments are left untouched and reported as an error.
- Do not report a successful detach when the compute API returns `Forbidden`.
- Detect active attachments when the compute API returns a fully qualified disk reference.

## [0.2.2] - 2026-08-04

### Fixed
- Corrected the Helm chart image digest, which pinned the v0.1.6 image while the tag read v0.2.1 — installs of chart 0.2.1 silently ran the v0.1.6 driver

## [0.2.1] - 2026-07-17


### Changed
- Fixed `cosign` signing script

## [0.2.0] - 2026-07-17

### Breaking:

- Username and password access is **deprecated** but is no longer recommended - username and password based access still works but will be removed in a later release

- The `.evroc.insecureTLS` configuration option has been removed from the credential file secrets

- The following configuration options are **deprecated** and have moved to a different path in the schema (the legacy paths are still usable for back-compatibility purposes but will be removed in a later release)
- `.evroc.restURL` (replaced by `.api.base_url`)
- `.evroc.organization` (replaced by `.context.organization`)
- `.evroc.project` (replaced by `.context.project`)
- `.evroc.insecureTLS` - fully removed
- `infrastructure.region` (replaced by `.context.region`)

### Added
- Support for service accounts and refresh tokens

### Changed

- Moved to using [evroc SDK](https://github.com/evroc-oss/evroc-go-sdk)


## [0.1.6] - 2026-02-18

### Changed

- Moved to evroc API version v1beta1
- Clarified support in `README.md`

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
- Automatic token refresh before token expiration
- Thread-safe token management

**Storage Backend:**
- REST API endpoint: `https://api.evroc.com`
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

[Unreleased]: https://github.com/evroc-oss/evroc-csi-driver/compare/v0.2.3...HEAD
[0.1.4]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.1.4
[0.1.5]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.1.5
[0.1.6]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.1.6
[0.2.0]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.2.0
[0.2.1]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.2.1
[0.2.2]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.2.2
[0.2.3]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v0.2.3
