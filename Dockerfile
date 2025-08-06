# Dockerfile: Full optimizer support (Go + Python)
#
# Use this for:
# - Both Go and Python optimizers
# - Development and testing
# - Production deployments that need optimizer flexibility
# - Default for make docker-build
#
# Build: docker build -t your-registry/inferno:full .
# 
# Build the manager binary (same as Dockerfile_GO)
FROM docker.io/golang:1.23 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Python runtime stage - supports both Go and Python optimizers
FROM python:3.11-slim AS runtime
WORKDIR /

# Install system dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy the manager binary from builder
COPY --from=builder /workspace/manager .

# Copy Python autoscaler code and install dependencies
COPY autoscaler/ /autoscaler/
RUN cd /autoscaler && pip install --no-cache-dir -r requirements.txt

# Create non-root user
RUN groupadd -r -g 65532 inferno && useradd -r -u 65532 -g inferno inferno
RUN chown -R 65532:65532 /autoscaler
USER 65532:65532

# Set environment variables - defaults to Go optimizer for backwards compatibility
ENV INFERNO_OPTIMIZER_TYPE=go
ENV INFERNO_PYTHON_PATH=python3
ENV INFERNO_PYTHON_SCRIPT=/autoscaler/cmd_folder/go_autoscaler_wrapper.py
ENV INFERNO_WORKING_DIR=/tmp
ENV PROMETHEUS_BASE_URL=""
ENV GLOBAL_OPT_INTERVAL=60s
ENV GLOBAL_OPT_TRIGGER=false

ENTRYPOINT ["/manager"]
