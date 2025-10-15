#!/usr/bin/env bash

set -euo pipefail

if ! command -v helm >/dev/null 2>&1; then
    echo "Helm is not installed. Please install it from https://helm.sh/docs/intro/install/" >&2
    exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
    echo "Kubectl is not installed. https://kubernetes.io/docs/reference/kubectl/" >&2
    exit 1
fi

DRY_RUN=${DRY_RUN:-"false"}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WVA_CHARTS_DIR="${ROOT_DIR}/charts/wva-llmd-infra"
PROMETHUS_VALUES_FILE=${ROOT_DIR}/config/samples/prometheus-adapter-values-ocp.yaml

# --- Output File ---
OUT_DIR=${TMP_DIR:-$(mktemp -d)}
mkdir -p "${OUT_DIR}"
WVA_VALUES_FILE="${OUT_DIR}/wva_config.yaml"

# --- WVA ---
WVA_NAMESPACE=${WVA_NAMESPACE:-workload-variant-autoscaler-system}
WVA_ENABLED=${WVA_ENABLED:-true}
WVA_BASE_NAME=${WVA_BASE_NAME:-"inference-scheduling"}
WVA_IMAGE_REPO=${WVA_IMAGE_REPO:-"ghcr.io/llm-d/workload-variant-autoscaler"}
WVA_IMAGE_TAG=${WVA_IMAGE_TAG:-"v0.0.1"}
WVA_REPLICA_COUNT=${WVA_REPLICA_COUNT:-1}
WVA_METRICS_ENABLED=${WVA_METRICS_ENABLED:-true}
WVA_METRICS_PORT=${WVA_METRICS_PORT:-8443}
WVA_METRICS_SECURE=${WVA_METRICS_SECURE:-true}
PROM_MONITORING_NAMESPACE=${PROM_MONITORING_NAMESPACE:-"openshift-user-workload-monitoring"}
PROM_BASE_URL=${PROM_BASE_URL:-"https://thanos-querier.openshift-monitoring.svc.cluster.local:9091"}
PROM_CA_CERT=${PROM_CA_CERT:-"BASE64_CERT_PLACEHOLDER"}

# --- LLMD ---
LLMD_NAMESPACE=${LLMD_NAMESPACE:-"llm-d-inference-scheduling"}
LLMD_MODEL_NAME=${LLMD_MODEL_NAME:-"ms-inference-scheduling-llm-d-modelservice"}
LLMD_MODEL_ID=${LLMD_MODEL_ID:-"unsloth/Meta-Llama-3.1-8B"}

# --- Variant Autoscaling ---
VA_ENABLED=${VA_ENABLED:-true}
VA_ACCELERATOR=${VA_ACCELERATOR:-"L40S"}
VA_SLO_TPOT=${VA_SLO_TPOT:-9}
VA_SLO_TTFT=${VA_SLO_TTFT:-1000}

# --- HPA ---
HPA_ENABLED=${HPA_ENABLED:-true}
HPA_MAX_REPLICAS=${HPA_MAX_REPLICAS:-10}
HPA_TARGET_AVERAGE_VALUE=${HPA_TARGET_AVERAGE_VALUE:-"1"}

# --- Guidellm ---
GUIDELLM_ENABLED=${GUIDELLM_ENABLED:-false}
GUIDELLM_IMAGE=${GUIDELLM_IMAGE:-"quay.io/vishakharamani/guidellm:latest"}
GUIDELLM_RATE=${GUIDELLM_RATE:-8}
GUIDELLM_TARGET=${GUIDELLM_TARGET:-"http://infra-inference-scheduling-inference-gateway:80"}
GUIDELLM_RATE_TYPE=${GUIDELLM_RATE_TYPE:-"constant"}
GUIDELLM_MAX_SECONDS=${GUIDELLM_MAX_SECONDS:-1800}

# --- VLLM Service ---
VLLM_ENABLED=${VLLM_ENABLED:-true}
VLLM_NODE_PORT=${VLLM_NODE_PORT:-30000}
VLLM_INTERVAL=${VLLM_INTERVAL:-"15s"}

cat <<EOF > "${WVA_VALUES_FILE}"
wva:
  enabled: ${WVA_ENABLED}
  baseName: ${WVA_BASE_NAME}
  image:
    repository: ${WVA_IMAGE_REPO}
    tag: ${WVA_IMAGE_TAG}
  replicaCount: ${WVA_REPLICA_COUNT}
  metrics:
    enabled: ${WVA_METRICS_ENABLED}
    port: ${WVA_METRICS_PORT}
    secure: ${WVA_METRICS_SECURE}
  prometheus:
    monitoringNamespace: ${PROM_MONITORING_NAMESPACE}
    baseURL: "${PROM_BASE_URL}"
    caCert: ${PROM_CA_CERT}
llmd:
  namespace: ${LLMD_NAMESPACE}
  modelName: ${LLMD_MODEL_NAME}
  modelID: "${LLMD_MODEL_ID}"
variantAutoscaling:
  enabled: ${VA_ENABLED}
  accelerator: ${VA_ACCELERATOR}
  sloTpot: ${VA_SLO_TPOT}
  sloTtft: ${VA_SLO_TTFT}
hpa:
  enabled: ${HPA_ENABLED}
  maxReplicas: ${HPA_MAX_REPLICAS}
  targetAverageValue: "${HPA_TARGET_AVERAGE_VALUE}"
guidellm:
  enabled: ${GUIDELLM_ENABLED}
  image: ${GUIDELLM_IMAGE}
  rate: ${GUIDELLM_RATE}
  target: "${GUIDELLM_TARGET}"
  rateType: "${GUIDELLM_RATE_TYPE}"
  maxSeconds: ${GUIDELLM_MAX_SECONDS}
vllmService:
  enabled: ${VLLM_ENABLED}
  nodePort: ${VLLM_NODE_PORT}
  interval: ${VLLM_INTERVAL}
EOF

function execute() {
    local _cmd=("$@")

    if [[ "${DRY_RUN}" == "true" ]]; then
        echo "[DRY-RUN] ${_cmd[*]}"
        return 0
    fi

    if ! "${_cmd[@]}"; then
        echo "Command failed: ${_cmd[*]}" >&2
        exit 1
    fi
}

kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${WVA_NAMESPACE}
  labels:
    app.kubernetes.io/name: workload-variant-autoscaler
    control-plane: controller-manager
EOF

kubectl apply -f "${WVA_CHARTS_DIR}"/crds/llmd.ai_variantautoscalings.yaml

execute helm upgrade -i workload-variant-autoscaler "${WVA_CHARTS_DIR}" \
    -n "${WVA_NAMESPACE}" \
    -f "${WVA_VALUES_FILE}" \
    --skip-crds

if [[ ${WVA_ENABLED} == "true" ]]; then
    execute helm repo add prometheus-community https://prometheus-community.github.io/helm-charts || true
    execute helm repo update
    execute helm upgrade -i prometheus-adapter prometheus-community/prometheus-adapter \
        -n "${PROM_MONITORING_NAMESPACE}" \
        -f "${PROMETHUS_VALUES_FILE}"
fi

exit 0
