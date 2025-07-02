#!/bin/bash

set -euo pipefail

cluster_name="a100-cluster"
control_plane_node="${cluster_name}-control-plane"
worker1_node="${cluster_name}-worker"
worker2_node="${cluster_name}-worker2"

echo "[1/3] Creating Kind cluster: ${cluster_name}..."

cat <<EOF > kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF

kind create cluster --name "${cluster_name}" --config kind-config.yaml

echo "[2/3] Waiting for node ${control_plane_node} to be ready..."
while [[ $(kubectl get nodes "${control_plane_node}" --no-headers 2>/dev/null | awk '{print $2}') != "Ready" ]]; do
  sleep 1
done

echo "[3/3] Patching node ${control_plane_node} with GPU annotation and capacity..."
cat <<EOF | kubectl patch node "${control_plane_node}" --type merge --patch "$(cat)"
metadata:
  labels:
    nvidia.com/gpu.product: NVIDIA-A100-PCIE-40GB
    nvidia.com/gpu.memory: "40960"
EOF

echo "Patching '${worker1_node}' with AMD GPU annotation..."
cat <<EOF | kubectl patch node "${worker1_node}" --type merge --patch "$(cat)"
metadata:
  labels:
    amd.com/gpu.product: AMD-RX-7800-XT
    amd.com/gpu.memory: "16384"
EOF

echo "Patching '${worker2_node}' with Intel GPU annotation..."
cat <<EOF | kubectl patch node "${worker2_node}" --type merge --patch "$(cat)"
metadata:
  labels:
    intel.com/gpu.product: Intel-Arc-A770
    intel.com/gpu.memory: "16384"
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

# Patch nodes
    for node_name in $(kubectl get nodes --no-headers -o custom-columns=":metadata.name")
    do
        echo "- Patching node (add): ${node_name}"
        if [[ "${node_name}" == "${worker1_node}" ]]; then
            resource_name="amd.com~1gpu"
            resource_count="6"
        elif [[ "${node_name}" == "${worker2_node}" ]]; then
            resource_name="intel.com~1gpu"
            resource_count="4"
        else
            resource_name="nvidia.com~1gpu"
            resource_count="8"
        fi

        curl --header "Content-Type: application/json-patch+json" \
             --request PATCH \
             --data '[{"op":"add","path":"/status/capacity/'${resource_name}'","value":"'${resource_count}'"}]' \
             http://localhost:8001/api/v1/nodes/${node_name}/status
    done

echo "[5/5] Cleaning up..."
kill -9 ${proxy_pid}

echo "🎉 Done: Nodes have GPU annotations, capacities, and allocatables set."
