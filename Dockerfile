# Multi-stage build for minimal image size

# Stage 1: Build the driver binary
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS builder

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
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    e2fsprogs \
    xfsprogs \
    blkid \
    util-linux

# Copy the binary from builder
COPY --from=builder /evroc-csi-driver /usr/local/bin/evroc-csi-driver

# Create necessary directories
RUN mkdir -p /var/lib/kubelet/plugins/disk.csi.evroc.com

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/evroc-csi-driver"]
