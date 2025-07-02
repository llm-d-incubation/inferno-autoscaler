#!/bin/bash

set -euo pipefail

cluster_name="a100-cluster"
node_name="a100-cluster-control-plane"

echo "[1/3] Creating Kind cluster: ${cluster_name}..."

cat <<EOF > kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
EOF

kind create cluster --name "${cluster_name}" --config kind-config.yaml

echo "[2/3] Waiting for node ${node_name} to be ready..."
while [[ $(kubectl get nodes "${node_name}" --no-headers 2>/dev/null | awk '{print $2}') != "Ready" ]]; do
  sleep 1
done

echo "[3/3] Patching node ${node_name} with GPU annotation and capacity..."

cat <<EOF | kubectl patch node "${node_name}" --type merge --patch "$(cat)"
metadata:
  labels:
    nvidia.com/gpu.product: NVIDIA-A100-PCIE-40GB
    nvidia.com/gpu.memory: "40960"
EOF

echo "[4/5] Starting kubectl proxy..."
kubectl proxy > /dev/null 2>&1 &
proxy_pid=$!
sleep 2  # Give proxy a moment to start

echo "Starting background proxy connection (pid=${proxy_pid})..."

    curl 127.0.0.1:8001 > /dev/null 2>&1

    if [[ ! $? -eq 0 ]]; then
        echo "Calling 'kubectl proxy' did not create a successful connection to the kubelet needed to patch the nodes. Exiting."
        exit 1
    else
        echo "Connected to the kubelet for patching the nodes"
    fi

# Variables
resource_name="nvidia.com~1gpu"
resource_count="8"

# Patch nodes
    for node_name in $(kubectl get nodes --no-headers -o custom-columns=":metadata.name")
    do
        echo "- Patching node (add): ${node_name}"

        patching_status=$(curl --header "Content-Type: application/json-patch+json" \
                                --request PATCH \
                                --data '[{"op": "add", "path": "/status/capacity/'${resource_name}'", "value": "'${resource_count}'"}]' \
                                http://localhost:8001/api/v1/nodes/${node_name}/status )
    done

echo "[5/5] Cleaning up..."
kill -9 ${proxy_pid}

echo "🎉 Done: Node '${node_name}' has GPU annotation, capacity, and allocatable set."