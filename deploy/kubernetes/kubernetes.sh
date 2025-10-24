#!/bin/bash
#
# Workload-Variant-Autoscaler Kubernetes Environment-Specific Configuration
# This script provides Kubernetes-specific functions and variable overrides
# It is sourced by the main install.sh script
# Note: it is not meant to be executed directly
#

set -e  # Exit on error
set -o pipefail  # Exit on pipe failure

#
# Kubernetes-specific Prometheus Configuration
# Note: overriding defaults from common script
#
PROMETHEUS_BASE_URL=${PROMETHEUS_BASE_URL:-"https://kube-prometheus-stack-prometheus.$MONITORING_NAMESPACE.svc.cluster.local"}
PROMETHEUS_PORT=${PROMETHEUS_PORT:-"9090"}
PROMETHEUS_URL=${PROMETHEUS_URL:-"$PROMETHEUS_BASE_URL:$PROMETHEUS_PORT"}
DEPLOY_PROMETHEUS=${DEPLOY_PROMETHEUS:-true}

check_specific_prerequisites() {
    log_info "Checking Kubernetes-specific prerequisites..."
    
    local missing_tools=()
    
    # Check for required tools (including Kubernetes-specific ones)
    for tool in kubectl helm yq; do
        if ! command -v $tool &> /dev/null; then
            missing_tools+=($tool)
        fi
    done
    
    if [ ${#missing_tools[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_info "Please install the missing tools and try again"
        exit 1
    fi
    
    log_success "All Kubernetes prerequisites met"
}

# Deploy WVA prerequisites for Kubernetes
deploy_wva_prerequisites() {
    log_info "Deploying Workload-Variant-Autoscaler prerequisites for Kubernetes..."

    # Extract Prometheus CA certificate
    log_info "Extracting Prometheus TLS certificate"
    kubectl get secret $PROMETHEUS_SECRET_NAME -n $MONITORING_NAMESPACE -o jsonpath='{.data.tls\.crt}' | base64 -d > $PROM_CA_CERT_PATH

    # Update Prometheus URL in configmap
    log_info "Updating Prometheus URL in config/manager/configmap.yaml"
    sed -i.bak "s|PROMETHEUS_BASE_URL:.*|PROMETHEUS_BASE_URL: \"$PROMETHEUS_URL\"|" config/manager/configmap.yaml

    log_success "WVA prerequisites complete"
}

# Deploy Prometheus on Kubernetes
deploy_prometheus_stack() {
    log_info "Deploying kube-prometheus-stack with TLS..."
    
    # Add helm repo
    helm repo add prometheus-community https://prometheus-community.github.io/helm-charts || true
    helm repo update
    
    # Create self-signed TLS certificate for Prometheus
    log_info "Creating self-signed TLS certificate for Prometheus"
    openssl req -x509 -newkey rsa:2048 -nodes \
        -keyout /tmp/prometheus-tls.key \
        -out /tmp/prometheus-tls.crt \
        -days 365 \
        -subj "/CN=prometheus" \
        -addext "subjectAltName=DNS:kube-prometheus-stack-prometheus.${MONITORING_NAMESPACE}.svc.cluster.local,DNS:kube-prometheus-stack-prometheus.${MONITORING_NAMESPACE}.svc,DNS:prometheus,DNS:localhost" \
        &> /dev/null
    
    # Create Kubernetes secret with TLS certificate
    log_info "Creating Kubernetes secret for Prometheus TLS"
    kubectl create secret tls $PROMETHEUS_SECRET_NAME \
        --cert=/tmp/prometheus-tls.crt \
        --key=/tmp/prometheus-tls.key \
        -n $MONITORING_NAMESPACE \
        --dry-run=client -o yaml | kubectl apply -f - &> /dev/null
    
    # Clean up temp files
    rm -f /tmp/prometheus-tls.{key,crt}
    
    # Install kube-prometheus-stack with TLS enabled
    log_info "Installing kube-prometheus-stack with TLS configuration"
    helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
        -n $MONITORING_NAMESPACE \
        --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
        --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
        --set prometheus.service.type=ClusterIP \
        --set prometheus.service.port=$PROMETHEUS_PORT \
        --set prometheus.prometheusSpec.web.tlsConfig.cert.secret.name=$PROMETHEUS_SECRET_NAME \
        --set prometheus.prometheusSpec.web.tlsConfig.cert.secret.key=tls.crt \
        --set prometheus.prometheusSpec.web.tlsConfig.keySecret.name=$PROMETHEUS_SECRET_NAME \
        --set prometheus.prometheusSpec.web.tlsConfig.keySecret.key=tls.key \
        --timeout=5m \
        --wait
    
    log_success "kube-prometheus-stack deployed with TLS"
    log_info "Prometheus URL: $PROMETHEUS_URL"
}

# Kubernetes-specific Undeployment functions
undeploy_prometheus_stack() {
    log_info "Uninstalling kube-prometheus-stack..."
    
    helm uninstall kube-prometheus-stack -n $MONITORING_NAMESPACE 2>/dev/null || \
        log_warning "Prometheus stack not found or already uninstalled"

    kubectl delete secret $PROMETHEUS_SECRET_NAME -n $MONITORING_NAMESPACE --ignore-not-found

    log_success "Prometheus stack uninstalled"
}

# Environment-specific functions are now sourced by the main install.sh script
# Do not call functions directly when sourced

