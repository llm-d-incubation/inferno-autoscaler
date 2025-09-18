#!/usr/bin/env bash

set -euo pipefail

INFRA_REPO_DIR="${HOME}/.cache/llm-d-infra"
PROJ_ROOT_DIR="$(pwd)"
ARCH=$(uname -m)

INTEGRATION_DIR="${PROJ_ROOT_DIR}/hack/vllme/deploy/integration_llm-d"
DEFAULT_VALUES_FILE="${INTEGRATION_DIR}/arm64-gaie-sim-values.yaml"
LLMD_NAMESPACE="llm-d-sim"
INFRA_RELEASE_NAME="infra-sim"
EPP_RELEASE_NAME="gaie-sim"

# Default values
WVA_IMAGE="quay.io/infernoautoscaler/inferno-controller:0.0.1-multi-arch"
CLUSTER_NODES="3"
CLUSTER_GPUS="4"
CLUSTER_TYPE="mix"

print_help() {
  cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Options:
  -i, --wva-image IMAGE        Container image to use for the WVA (default: quay.io/infernoautoscaler/inferno-controller:0.0.1-multi-arch)
  -n, --nodes NUM              Number of nodes for KIND cluster (default: 3)
  -g, --gpus NUM               Number of GPUs per node (default: 4)  
  -t, --type TYPE              GPU type: nvidia, amd, intel, or mix (default: nvidia)
  -h, --help                   Show this help and exit

Examples:
  # Deploy with default values
  $(basename "$0")
  
  # Deploy with custom inferno image and cluster configuration
  $(basename "$0") -i my-registry/inferno:latest -n 3 -g 6 -t mix
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -i|--inferno-image)     WVA_IMAGE="$2"; shift 2 ;;
      -n|--nodes)             CLUSTER_NODES="$2"; shift 2 ;;
      -g|--gpus)              CLUSTER_GPUS="$2"; shift 2 ;;
      -t|--type)              CLUSTER_TYPE="$2"; shift 2 ;;
      -h|--help)              print_help; exit 0 ;;
      *)                      echo "Unknown option: $1"; print_help; exit 1 ;;
    esac
  done
}

get-llm-d-latest() {
  if [ -d "$INFRA_REPO_DIR" ]; then
    echo ">>> Removing any existing llm-d infrastructure repo at $INFRA_REPO_DIR"
    rm -rf "$INFRA_REPO_DIR"
  fi

  local owner="llm-d-incubation" 
  local project="llm-d-infra"
  local release="v1.3.1"

  echo ">>> Cloning the latest release of $project from $owner: $release"
  echo ">>> Cloning into $INFRA_REPO_DIR"
  git clone -b $release -- https://github.com/$owner/$project.git $INFRA_REPO_DIR
}

function apply_fix_for_vllme_comp() {
  local INFERENCE_MODEL_FILE="${INTEGRATION_DIR}/vllme-inferencemodel.yaml"

  echo ">>> Applying fixes to integrate vLLM emulator servers..."
  echo ">>> Applying InferenceModel CR ..."
  kubectl apply -f "$INFERENCE_MODEL_FILE"

  echo ">>> Patching InferencePool to target vLLM emulator port ..."
  kubectl patch inferencepool "$EPP_RELEASE_NAME" -n "$LLMD_NAMESPACE" --type='merge' -p '{"spec":{"targetPortNumber":80}}'

  echo ">>> Deleting other SIM deployments if they exist..."
  kubectl delete deployments.apps ms-sim-llm-d-modelservice-decode ms-sim-llm-d-modelservice-prefill --ignore-not-found -n "$LLMD_NAMESPACE"

  echo ">>> Applying ConfigMap for vLLM emulator integration..."
  kubectl apply -f - <<EOF
apiVersion: v1
data:
  default-plugins.yaml: |
    apiVersion: inference.networking.x-k8s.io/v1alpha1
    kind: EndpointPickerConfig
    plugins:
    - type: low-queue-filter
      parameters:
        threshold: 128
    - type: lora-affinity-filter
      parameters:
        threshold: 0.999
    - type: least-queue-filter
    - type: least-kv-cache-filter
    - type: decision-tree-filter
      name: low-latency-filter
      parameters:
        current:
          pluginRef: low-queue-filter
        nextOnSuccess:
          decisionTree:
            current:
              pluginRef: lora-affinity-filter
            nextOnSuccessOrFailure:
              decisionTree:
                current:
                  pluginRef: least-queue-filter
                nextOnSuccessOrFailure:
                  decisionTree:
                    current:
                      pluginRef: least-kv-cache-filter
        nextOnFailure:
          decisionTree:
            current:
              pluginRef: least-queue-filter
            nextOnSuccessOrFailure:
              decisionTree:
                current:
                  pluginRef: lora-affinity-filter
                nextOnSuccessOrFailure:
                  decisionTree:
                    current:
                      pluginRef: least-kv-cache-filter
    - type: random-picker
      parameters:
        maxNumOfEndpoints: 1
    - type: single-profile-handler
    schedulingProfiles:
    - name: default
      plugins:
      - pluginRef: low-latency-filter
      - pluginRef: random-picker
  plugins-v2.yaml: |
    apiVersion: inference.networking.x-k8s.io/v1alpha1
    kind: EndpointPickerConfig
    plugins:
    - type: queue-scorer
    - type: kv-cache-scorer
    # - type: prefix-cache-scorer
      parameters:
        hashBlockSize: 64
        maxPrefixBlocksToMatch: 256
        lruCapacityPerServer: 31250
    - type: max-score-picker
      parameters:
        maxNumOfEndpoints: 1
    - type: single-profile-handler
    schedulingProfiles:
    - name: default
      plugins:
      - pluginRef: queue-scorer
        weight: 1
      - pluginRef: kv-cache-scorer
        weight: 1
      # - pluginRef: prefix-cache-scorer
      #   weight: 1
      - pluginRef: max-score-picker
kind: ConfigMap
metadata:
  annotations:
    meta.helm.sh/release-name: gaie-inference-scheduling
    meta.helm.sh/release-namespace: llm-d-inference-scheduler
  labels:
    app.kubernetes.io/managed-by: Helm
  name: gaie-inference-scheduling-epp
  namespace: $LLMD_NAMESPACE
EOF
}

function deploy_inferno() {
    echo ">>> Deploying Inferno Autoscaler using image: $WVA_IMAGE"
    make deploy-inferno-emulated-on-kind IMG="${WVA_IMAGE}" KIND_ARGS="-n ${CLUSTER_NODES} -g ${CLUSTER_GPUS} -t ${CLUSTER_TYPE}"
    echo "Inferno Autoscaler deployed successfully."
}

function deploy-llm-d-infra() {
  local GATEWAY="kgateway"
  local NAMESPACE="$LLMD_NAMESPACE"
  local DEPS_DIR="dependencies"
  local GATEWAY_DIR="gateway-control-plane-providers"
  local SIM_DIR="$INFRA_REPO_DIR/quickstart/examples/sim"

  echo ">>> Running the dependency script"
  bash "$DEPS_DIR/install-deps.sh"

  echo ">>> Installing Gateway API CRDs"
  bash "$GATEWAY_DIR/install-gateway-provider-dependencies.sh"

  # Changing kgateway version to 2.0.3
  yq eval '.releases[].version = "v2.0.3"' -i "$GATEWAY_DIR/kgateway.helmfile.yaml"

  echo ">>> Installing Gateway-specific CRDs (kgateway)"
  helmfile apply -f "$GATEWAY_DIR/kgateway.helmfile.yaml"

  cd "$SIM_DIR"

  echo ">>> Installing the llm-d infrastructure for simulation"
  export HF_TOKEN="dummy-token"
  helmfile apply -e kgateway
  sleep 30
  echo ">>> llm-d infrastructure installed."
}

echo ">>> Getting latest llm-d infrastructure release..."
get-llm-d-latest

main() {
parse_args "$@"

echo ">>> Using configuration:"
echo "    WVA Image: $WVA_IMAGE"
echo "    Cluster Nodes: $CLUSTER_NODES"
echo "    Cluster GPUs: $CLUSTER_GPUS"
echo "    Cluster Type: $CLUSTER_TYPE"

if [[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]]; then
  echo "ARM64 platform detected, using custom arm64 values.yaml"
  cp "$DEFAULT_VALUES_FILE" $INFRA_REPO_DIR/quickstart/examples/sim/gaie-sim/values.yaml

else
  echo "Non-ARM64 platform, using default manifest."

fi

cd "$PROJ_ROOT_DIR"
deploy_inferno

cd "$INFRA_REPO_DIR/quickstart"
deploy-llm-d-infra

apply_fix_for_vllme_comp

echo "llm-d infrastructure installation complete."

echo ">>> To target the deployed vLLM-emulator servers, deploy the VariantAutoscaling object:"
echo "kubectl apply -f hack/vllme/deploy/vllme-setup/vllme-variantautoscaling.yaml"

echo ">>> To curl the Gateway, port-forward it first using:"
echo "kubectl port-forward -n $LLMD_NAMESPACE svc/$INFRA_RELEASE_NAME-inference-gateway 8000:80"

echo ">>> Then launch the load generator:"
echo "cd $PROJ_ROOT_DIR/hack/vllme/vllm_emulator"
echo "pip install -r requirements.txt"
echo "python loadgen.py --model default/default  --rate '[[1200, 40]]' --url http://localhost:8000/v1 --content 50"
}

main "$@"