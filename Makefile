# Makefile for evroc CSI Driver

# Variables
DRIVER_NAME = evroc-csi-driver
DOCKER_REGISTRY ?= ghcr.io/evroc-oss
IMAGE_NAME = $(DOCKER_REGISTRY)/$(DRIVER_NAME)
VERSION ?= $(shell cat VERSION 2>/dev/null || echo "v0.0.0-unknown")
GIT_COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE = $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build variables
BUILD_DIR = bin
MAIN_PATH = cmd/evroc-csi-driver/main.go
BINARY = $(BUILD_DIR)/$(DRIVER_NAME)

# Kubernetes variables
NAMESPACE ?= kube-system
ENDPOINT ?= /tmp/csi.sock
LOG_LEVEL ?= info
LOG_JSON ?= false

# Go variables
GOFILES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

.PHONY: all
all: lint test build

.PHONY: help
help:
	@echo "evroc CSI Driver - Makefile targets:"
	@echo ""
	@echo "Build & Development:"
	@echo "  make build            - Build the driver binary"
	@echo "  make build-tools      - Build all CLI tools (rest-tool)"
	@echo "  make rest-tool        - Build rest-tool CLI"
	@echo "  make run              - Run the driver locally"
	@echo "  make docker           - Build Docker image"
	@echo "  make push             - Push Docker image to registry"
	@echo ""
	@echo "Testing & Quality:"
	@echo "  make test             - Run unit tests"
	@echo "  make test-sanity      - Run CSI sanity test suite"
	@echo "  make test-all         - Run all tests (unit + sanity)"
	@echo "  make test-basic       - Test basic create/delete operations"
	@echo "  make lint             - Run linters"
	@echo "  make verify           - Run all verification checks (fmt, vet, lint, test)"
	@echo ""
	@echo "Kubernetes:"
	@echo "  make install          - Install driver to Kubernetes cluster"
	@echo "  make uninstall        - Uninstall driver from Kubernetes cluster"
	@echo "  make logs             - View driver logs from Kubernetes"
	@echo "  make dev-reload       - Quick rebuild and reload for k3d development"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean            - Clean build artifacts"
	@echo "  make version          - Show current version info"
	@echo "  make help             - Show this help message"

.PHONY: build
build: $(BUILD_DIR)
	@echo "Building $(DRIVER_NAME) $(VERSION)..."
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags "-s -w -X github.com/evroc-oss/evroc-csi-driver/pkg/version.Version=$(VERSION) \
		          -X github.com/evroc-oss/evroc-csi-driver/pkg/version.GitCommit=$(GIT_COMMIT) \
		          -X github.com/evroc-oss/evroc-csi-driver/pkg/version.BuildDate=$(BUILD_DATE)" \
		-o $(BINARY) $(MAIN_PATH)
	@echo "Binary built: $(BINARY)"

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

.PHONY: build-tools
build-tools: rest-tool
	@echo "All tools built successfully"

.PHONY: rest-tool
rest-tool: $(BUILD_DIR)
	@echo "Building rest-tool..."
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-o $(BUILD_DIR)/rest-tool tools/rest-tool/main.go
	@echo "Binary built: $(BUILD_DIR)/rest-tool"

.PHONY: run
run: build
	@echo "Running $(DRIVER_NAME)..."
	@echo "CSI endpoint: $(ENDPOINT)"
	@echo "Log level: $(LOG_LEVEL)"
	$(BINARY) --endpoint=$(ENDPOINT) --node-id=$$(hostname) --log-level=$(LOG_LEVEL) $(if $(filter true,$(LOG_JSON)),--log-json)

.PHONY: test
test:
	@echo "Running unit tests..."
	go test -v -race -coverprofile=coverage.txt -covermode=atomic ./pkg/...

.PHONY: test-sanity
test-sanity:
	@echo "Running CSI sanity test suite..."
	go test -v -timeout=10m ./test/sanity/... -args -ginkgo.v

.PHONY: test-all
test-all: test test-sanity
	@echo "All tests completed!"

.PHONY: test-basic
test-basic:
	@echo "Running basic operations test..."
	./scripts/test-basic-operations.sh

.PHONY: lint
lint:
	@echo "Running linters..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install it from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run --timeout=5m

.PHONY: fmt
fmt:
	@echo "Formatting Go code..."
	gofmt -s -w $(GOFILES)

.PHONY: vet
vet:
	@echo "Running go vet..."
	go vet ./...

.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.txt
	rm -f $(ENDPOINT)

.PHONY: docker
docker:
	@echo "Building Docker image $(IMAGE_NAME):$(VERSION)..."
	docker build -t $(IMAGE_NAME):$(VERSION) .
	docker tag $(IMAGE_NAME):$(VERSION) $(IMAGE_NAME):latest
	@echo "Docker image built: $(IMAGE_NAME):$(VERSION)"

.PHONY: push
push: docker
	@echo "Pushing Docker image $(IMAGE_NAME):$(VERSION)..."
	docker push $(IMAGE_NAME):$(VERSION)
	docker push $(IMAGE_NAME):latest

.PHONY: generate
generate:
	@echo "Running code generation..."
	@echo "Note: CSI proto generation not needed as we use the official spec"
	@echo "Add custom generation commands here if needed in the future"

.PHONY: manifest
manifest:
	@echo "Kubernetes manifests are available in deploy/kubernetes/"
	@echo "To customize, edit the files in that directory"

.PHONY: install
install:
	@echo "Installing $(DRIVER_NAME) to Kubernetes cluster..."
	kubectl apply -f deploy/kubernetes/csidriver.yaml
	kubectl apply -f deploy/kubernetes/rbac.yaml
	kubectl apply -f deploy/kubernetes/controller.yaml
	kubectl apply -f deploy/kubernetes/daemonset.yaml
	@echo "Waiting for Controller to be ready..."
	kubectl wait --for=condition=ready pod -l app=evroc-csi-controller -n $(NAMESPACE) --timeout=120s
	@echo "Waiting for DaemonSet to be ready..."
	kubectl wait --for=condition=ready pod -l app=$(DRIVER_NAME) -n $(NAMESPACE) --timeout=120s
	@echo "$(DRIVER_NAME) installed successfully!"

.PHONY: uninstall
uninstall:
	@echo "Uninstalling $(DRIVER_NAME) from Kubernetes cluster..."
	kubectl delete -f deploy/kubernetes/daemonset.yaml --ignore-not-found=true
	kubectl delete -f deploy/kubernetes/controller.yaml --ignore-not-found=true
	kubectl delete -f deploy/kubernetes/rbac.yaml --ignore-not-found=true
	kubectl delete -f deploy/kubernetes/csidriver.yaml --ignore-not-found=true
	@echo "$(DRIVER_NAME) uninstalled successfully!"

.PHONY: logs
logs:
	@echo "Fetching logs from $(DRIVER_NAME) pods..."
	kubectl logs -l app=$(DRIVER_NAME) -n $(NAMESPACE) --all-containers=true --tail=100 -f --max-log-requests=20

.PHONY: dev-reload
dev-reload: docker
	@echo "Importing image to k3d cluster..."
	k3d image import $(IMAGE_NAME):$(VERSION) -c mycluster
	@echo "Restarting driver pods..."
	kubectl delete pod -n $(NAMESPACE) -l app=$(DRIVER_NAME)
	kubectl delete pod -n $(NAMESPACE) -l app=evroc-csi-controller
	@echo "Waiting for pods to restart..."
	kubectl wait --for=condition=ready pod -l app=$(DRIVER_NAME) -n $(NAMESPACE) --timeout=60s
	kubectl wait --for=condition=ready pod -l app=evroc-csi-controller -n $(NAMESPACE) --timeout=60s
	@echo "Pods restarted! Use 'make logs' to view logs"

.PHONY: verify
verify: fmt vet lint test
	@echo "Verification complete!"

.PHONY: mod-download
mod-download:
	@echo "Downloading Go modules..."
	go mod download

.PHONY: mod-tidy
mod-tidy:
	@echo "Tidying Go modules..."
	go mod tidy

.PHONY: version
version:
	@echo "Version:    $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
