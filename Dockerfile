# Multi-stage build for minimal image size

# Stage 1: Build the driver binary
# Go 1.27.3 fixes CVE-2026-111943 (crypto/x509 blocks the compromised eMudhra
# intermediate CAs). The fix is compiled into the binary, so the toolchain
# must be kept at a patched version (1.26.10+ / 1.27.3+).
FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder

# Build arguments for version information
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY VERSION ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s \
    -X github.com/evroc-oss/evroc-csi-driver/pkg/version.Version=${VERSION} \
    -X github.com/evroc-oss/evroc-csi-driver/pkg/version.GitCommit=${GIT_COMMIT} \
    -X github.com/evroc-oss/evroc-csi-driver/pkg/version.BuildDate=${BUILD_DATE}" \
    -o /evroc-csi-driver \
    ./cmd/evroc-csi-driver/main.go

# Stage 2: Create minimal runtime image
# Alpine 3.23.7+ ships ca-certificates 20261020-r0, which removes the
# compromised eMudhra roots from the trust store.
FROM alpine:3.23@sha256:ff7f0c727dd796aa36787bfcd9a64bf35a6ce72a73451b3359fde849b6703d63

# Install runtime dependencies
# Note: resize2fs is in e2fsprogs-extra and xfs_growfs is in xfsprogs-extra
# (the base e2fsprogs/xfsprogs packages only provide mkfs/fsck, not the
# resize tools needed by NodeExpandVolume).
RUN apk add --no-cache \
    ca-certificates \
    e2fsprogs \
    e2fsprogs-extra \
    xfsprogs \
    xfsprogs-extra \
    blkid \
    util-linux

# Copy the binary from builder
COPY --from=builder /evroc-csi-driver /usr/local/bin/evroc-csi-driver

# Create necessary directories
RUN mkdir -p /var/lib/kubelet/plugins/disk.csi.evroc.com

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/evroc-csi-driver"]
