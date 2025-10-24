#!/bin/bash
#
# Workload-Variant-Autoscaler OpenShift Environment-Specific Configuration
# This script provides OpenShift-specific functions and variable overrides
# It is sourced by the main install.sh script
# Note: it is not meant to be executed directly
#

set -e  # Exit on error
set -o pipefail  # Exit on pipe failure

#
# OpenShift-specific Prometheus Configuration
# Note: overriding defaults from common script
#
PROMETHEUS_BASE_URL=${PROMETHEUS_BASE_URL:-"https://thanos-querier.openshift-monitoring.svc.cluster.local"}
PROMETHEUS_PORT=${PROMETHEUS_PORT:-"9091"}
PROMETHEUS_URL=${PROMETHEUS_URL:-"$PROMETHEUS_BASE_URL:$PROMETHEUS_PORT"}
DEPLOY_PROMETHEUS=false  # OpenShift has built-in monitoring

check_specific_prerequisites() {
    log_info "Checking OpenShift-specific prerequisites..."
    
    local missing_tools=()
    
    # Check for required tools (including OpenShift-specific ones)
    for tool in oc kubectl helm yq; do
        if ! command -v $tool &> /dev/null; then
            missing_tools+=($tool)
        fi
    done
    
    if [ ${#missing_tools[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_info "Please install the missing tools and try again"
        exit 1
    fi
    
    # Check OpenShift connection
    if ! oc whoami &> /dev/null; then
        log_error "Not logged into OpenShift cluster. Please run 'oc login' first"
        exit 1
    fi
    
    log_success "All OpenShift prerequisites met"
    log_info "Connected to OpenShift as: $(oc whoami)"
    log_info "Current project: $(oc project -q)"
}

find_thanos_url() {
    log_info "Finding Thanos querier URL..."
    
    local thanos_svc=$(kubectl get svc -n openshift-monitoring thanos-querier -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    
    # Set PROMETHEUS_URL if Thanos service is found
    if [ -n "$thanos_svc" ]; then
        PROMETHEUS_URL="${PROMETHEUS_BASE_URL}:${PROMETHEUS_PORT}"
        log_success "Found Thanos querier: $PROMETHEUS_URL"
    else
        log_error "Thanos querier service not found in openshift-monitoring namespace"
        exit 1
    fi
    
    export PROMETHEUS_URL
}

# OpenShift uses existing monitoring stack (Thanos/Prometheus)
deploy_prometheus_stack() {
    log_info "Using OpenShift built-in monitoring (Thanos)..."
    find_thanos_url
    log_success "OpenShift monitoring stack is available"
}

deploy_wva_prerequisites() {
    log_info "Deploying Workload-Variant-Autoscaler..."

    # Extract Thanos TLS certificate
    log_info "Extracting Thanos TLS certificate"
    kubectl get secret thanos-querier-tls -n openshift-monitoring -o jsonpath='{.data.tls\.crt}' | base64 -d > $PROM_CA_CERT_PATH

    # Update Prometheus URL in configmap
    log_info "Updating Prometheus URL in config/manager/configmap.yaml"
    sed -i.bak "s|PROMETHEUS_BASE_URL:.*|PROMETHEUS_BASE_URL: \"$PROMETHEUS_URL\"|" config/manager/configmap.yaml
    
    log_success "WVA prerequisites deployed"
}

# OpenShift uses built-in monitoring, nothing to undeploy
undeploy_prometheus_stack() {
    log_info "OpenShift uses built-in monitoring stack (no cleanup needed)"
}

# Environment-specific functions are now sourced by the main install.sh script
# Do not call functions directly when sourced

