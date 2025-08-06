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

get-llm-d-latest() {
  if [ -d "$INFRA_REPO_DIR" ]; then
    echo ">>> Removing any existing llm-d infrastructure repo at $INFRA_REPO_DIR"
    rm -rf "$INFRA_REPO_DIR"
  fi

  local OWNER="llm-d-incubation" 
  local PROJECT="llm-d-infra"
  local RELEASE_URL=$(curl -Ls -o /dev/null -w %{url_effective} https://github.com/$OWNER/$PROJECT/releases/latest)
  local RELEASE_TAG=$(basename $RELEASE_URL)
  
  echo ">>> Cloning the latest release of $PROJECT from $OWNER: $RELEASE_TAG"
  echo ">>> Cloning into $INFRA_REPO_DIR"
  git clone -b $RELEASE_TAG -- https://github.com/$OWNER/$PROJECT.git $INFRA_REPO_DIR 
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
}

function deploy_inferno() {
    echo ">>> Deploying Inferno Autoscaler..."
    make deploy-inferno-emulated-on-kind "$@"
    echo "Inferno Autoscaler deployed successfully."
}

function deploy-llm-d-infra() {
  local GATEWAY="kgateway"

  echo ">>> Running the dependency script"
  bash install-deps.sh

  echo ">>> Running the llm-d installer script"
  export HF_TOKEN="dummy-token"
  ./llmd-infra-installer.sh --namespace "$LLMD_NAMESPACE" -r "$INFRA_RELEASE_NAME" --gateway "$GATEWAY" --disable-metrics-collection

  echo ">>> Use the helmfile to apply the modelservice and GIE charts on top of it."
  cd "$INFRA_REPO_DIR/quickstart/examples/sim"
  helmfile --selector managedBy=helmfile apply -f helmfile.yaml --skip-diff-on-install

  echo ">>> Waiting for the llm-d sim EPP and Gateway to be ready..."
  kubectl wait --for=create -n "$LLMD_NAMESPACE" deployment/"$INFRA_RELEASE_NAME"-inference-gateway --timeout=30s
  kubectl wait --for=create -n "$LLMD_NAMESPACE" deployment/"$EPP_RELEASE_NAME"-epp --timeout=30s
  kubectl wait --for=condition=Available -n "$LLMD_NAMESPACE" deployment/"$INFRA_RELEASE_NAME"-inference-gateway --timeout=60s
  kubectl wait --for=condition=Available -n "$LLMD_NAMESPACE" deployment/"$EPP_RELEASE_NAME"-epp --timeout=60s
  echo ">>> Gateway and EPP are ready."
}

echo ">>> Getting latest llm-d infrastructure release..."
get-llm-d-latest

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
echo "python loadgen.py"
echo ">>> As 'server base URL', use: http://localhost:8000/v1 [option 3]"
echo ">>> As 'model name', insert: vllm"
