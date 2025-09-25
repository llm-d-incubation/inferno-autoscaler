#!/usr/bin/env bash

set -eou pipefail

KIND=${KIND:-kind}
KUBECTL=${KUBECTL:-kubectl}
KIND_NAME=${KIND_NAME:-"kind-inferno-gpu-cluster"}
KIND_CONTEXT=kind-${KIND_NAME}
MONITORING_NAMESPACE=${MONITORING_NAMESPACE:-"workload-variant-autoscaler-monitoring"}
KIND_NODE_NAME=${KIND_NODE_NAME:-"kind-inferno-gpu-cluster-control-plane"}
WEBHOOK_TIMEOUT=${WEBHOOK_TIMEOUT:-3m}
LLMD_NAMESPACE=${LLMD_NAMESPACE:-"llm-d-sim"}
FORCE_NEW_CERT=${FORCE_NEW_CERT:-"false"}
PROMETHEUS_BASE_URL=${PROMETHEUS_BASE_URL:-"https://kube-prometheus-stack-prometheus.workload-variant-autoscaler-monitoring.svc.cluster.local:9090"}

_kubectl() {
        ${KUBECTL} --context ${KIND_CONTEXT} $@
}

_kind() {
	${KIND} $@
}

# Local development will need emulated vllm server, prometheus installed in KinD cluster
_kubectl create ns ${MONITORING_NAMESPACE} 2>/dev/null || true
_kubectl create ns ${LLMD_NAMESPACE} 2>/dev/null || true

# Generate TLS certificates for Prometheus
echo "Configuring Prometheus with HTTPS/TLS..."

# Create TLS certificates directory
mkdir -p hack/tls-certs

# Check if certificate already exists and is valid
CERT_FILE="hack/tls-certs/prometheus-cert.pem"
KEY_FILE="hack/tls-certs/prometheus-key.pem"
CERT_EXPIRY_DAYS=3650  # 10 years

if [[ "$FORCE_NEW_CERT" == "true" ]]; then
    echo "FORCE_NEW_CERT=true, generating new certificate..."
    CERT_EXISTS=false
elif [[ -f "$CERT_FILE" && -f "$KEY_FILE" ]]; then
    echo "Checking existing certificate validity..."
    
    # Check if certificate is still valid (not expired)
    if openssl x509 -checkend 86400 -noout -in "$CERT_FILE" >/dev/null 2>&1; then
        echo "Valid certificate found, using existing certificate..."
        CERT_EXISTS=true
    else
        echo "Certificate expired or will expire soon, generating new certificate..."
        CERT_EXISTS=false
    fi
else
    echo "No certificate found, generating new certificate..."
    CERT_EXISTS=false
fi

# Generate self-signed certificate if needed
if [[ "$CERT_EXISTS" != "true" ]]; then
    echo "Generating self-signed certificate for Prometheus (valid for ${CERT_EXPIRY_DAYS} days)..."
    openssl req -x509 -newkey rsa:4096 -keyout "$KEY_FILE" -out "$CERT_FILE" -days ${CERT_EXPIRY_DAYS} -nodes -subj "/CN=prometheus" -addext "subjectAltName=DNS:kube-prometheus-stack-prometheus.workload-variant-autoscaler-monitoring.svc.cluster.local,DNS:kube-prometheus-stack-prometheus.workload-variant-autoscaler-monitoring.svc,DNS:prometheus,DNS:localhost,IP:127.0.0.1"
    
    if [[ $? -eq 0 ]]; then
        echo "Certificate generated successfully"
    else
        echo "ERROR: Failed to generate certificate"
        exit 1
    fi
fi

# Create Kubernetes secret for TLS certificates
echo "Creating Kubernetes secret for TLS certificates..."
_kubectl create secret tls prometheus-tls --cert="$CERT_FILE" --key="$KEY_FILE" -n ${MONITORING_NAMESPACE} 2>/dev/null || true

# Install Prometheus using Helm with TLS configuration
echo "Installing Prometheus with TLS configuration..."
# Configure Prometheus URL and TLS settings for the Kind cluster deployment
echo "Configuring Prometheus URL and TLS settings for Kind cluster deployment..."
echo "Using Prometheus URL: ${PROMETHEUS_BASE_URL}"
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm upgrade -i kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -n ${MONITORING_NAMESPACE} \
  -f hack/vllme/deploy/prometheus-operator/prometheus-tls-values.yaml

_kubectl apply -f hack/vllme/deploy/prometheus-operator/prometheus-deploy-all-in-one.yaml

# Create vllm emulated deployment
_kubectl apply -f hack/vllme/deploy/vllme-setup/vllme-deployment-with-service-and-servicemon.yaml