#!/bin/bash

set -euo pipefail

# --------------------------------------------------------------------
# Defaults
# --------------------------------------------------------------------
DEFAULT_CLUSTER_NAME="kind-inferno-gpu-cluster"
DEFAULT_NODES=3
DEFAULT_GPUS_PER_NODE=2
DEFAULT_GPU_TYPE="mix"
DEFAULT_GPU_MODEL="NVIDIA-A100-PCIE-80GB"
DEFAULT_GPU_MEMORY=81920

# Initialize variables
cluster_name="$DEFAULT_CLUSTER_NAME"
nodes="$DEFAULT_NODES"
gpus_per_node="$DEFAULT_GPUS_PER_NODE"
gpu_type="$DEFAULT_GPU_TYPE"
gpu_model="$DEFAULT_GPU_MODEL"
gpu_memory="$DEFAULT_GPU_MEMORY"

# --------------------------------------------------------------------
# Cleanup on exit
# --------------------------------------------------------------------
cleanup() {
    if [[ -n "${proxy_pid:-}" ]]; then
        kill "$proxy_pid" &>/dev/null || true
        # wait may return 143 (SIGTERM), ignore it
        wait "$proxy_pid" 2>/dev/null || true
    fi
    [[ -f "kind-config.yaml" ]] && rm -f "kind-config.yaml" || true
    return 0
}
trap cleanup EXIT

# --------------------------------------------------------------------
# Usage
# --------------------------------------------------------------------
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Options:
    -c CLUSTER_NAME    Cluster name (default: $DEFAULT_CLUSTER_NAME)
    -n NODES           Number of nodes (default: $DEFAULT_NODES)
    -g GPUS            GPUs per node (default: $DEFAULT_GPUS_PER_NODE)
    -t TYPE            GPU type: nvidia, amd, intel, mix (default: $DEFAULT_GPU_TYPE)
    -d MODEL           GPU model (default: $DEFAULT_GPU_MODEL)
    -m MEMORY          GPU memory in MB (default: $DEFAULT_GPU_MEMORY)
    -h                 Show this help message
EOF
}

validate_gpu_type() {
    case "$1" in
        nvidia|amd|intel|mix) return 0 ;;
        *)
            echo "Error: Invalid GPU type '$1'. Valid: nvidia, amd, intel, mix"
            exit 1
            ;;
    esac
}

# --------------------------------------------------------------------
# Parse Args
# --------------------------------------------------------------------
while getopts "c:n:g:t:d:m:h" opt; do
    case $opt in
        c) cluster_name="$OPTARG" ;;
        n) nodes="$OPTARG" ;;
        g) gpus_per_node="$OPTARG" ;;
        t) gpu_type="$OPTARG"; validate_gpu_type "$gpu_type" ;;
        d) gpu_model="$OPTARG" ;;
        m) gpu_memory="$OPTARG" ;;
        h) usage; exit 0 ;;
        *) usage; exit 1 ;;
    esac
done

# --------------------------------------------------------------------
# Create Kind Cluster
# --------------------------------------------------------------------
echo "[1/6] Creating Kind cluster: ${cluster_name} with ${nodes} nodes and ${gpus_per_node} GPUS each..."

cat <<EOF > kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
EOF

for ((i=1; i<nodes; i++)); do
    echo "- role: worker" >> kind-config.yaml
done

kind create cluster --name "${cluster_name}" --config kind-config.yaml

control_plane_node="${cluster_name}-control-plane"
echo "[2/6] Waiting for node ${control_plane_node} to be ready..."
while [[ $(kubectl get nodes "${control_plane_node}" --no-headers 2>/dev/null | awk '{print $2}') != "Ready" ]]; do
    sleep 1
done

echo "[2.1/6] Removing control-plane node taint to allow scheduling..."

kubectl taint nodes "${control_plane_node}" node-role.kubernetes.io/control-plane- || true

# --------------------------------------------------------------------
# Patch Node Labels
# --------------------------------------------------------------------
patch_node_gpu() {
    local node_name="$1"
    local gpu_type="$2"
    local gpu_count="$3"
    local gpu_product="$4"
    local gpu_memory="$5"

    kubectl patch node "${node_name}" --type merge --patch "
metadata:
  labels:
    ${gpu_type}.com/gpu.count: \"${gpu_count}\"
    ${gpu_type}.com/gpu.product: \"${gpu_product}\"
    ${gpu_type}.com/gpu.memory: \"${gpu_memory}\"
"
}

nodes_list=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name")
node_array=($nodes_list)

for i in "${!node_array[@]}"; do
    node_name="${node_array[$i]}"

    if [ "$gpu_type" != "mix" ]; then
        current_type="$gpu_type"
        current_model="$gpu_model"
        current_memory="$gpu_memory"
    else
        case $((i % 3)) in
            0) current_type="nvidia"; current_model="NVIDIA-A100-PCIE-80GB"; current_memory=81920 ;;
            1) current_type="amd";    current_model="AMD-MI300X-192G";       current_memory=196608 ;;
            2) current_type="intel";  current_model="Intel-Gaudi-2-96GB";    current_memory=98304 ;;
        esac
    fi

    patch_node_gpu "$node_name" "$current_type" "$gpus_per_node" "$current_model" "$current_memory"
done

# --------------------------------------------------------------------
# Patch Node Capacities
# --------------------------------------------------------------------
echo "[3/6] Starting kubectl proxy..."
kubectl proxy > /dev/null 2>&1 &
proxy_pid=$!
for i in {1..30}; do
    if curl -s 127.0.0.1:8001/api/v1 > /dev/null 2>&1; then break; fi
    sleep 1
done

for i in "${!node_array[@]}"; do
    node_name="${node_array[$i]}"
    if [ "$gpu_type" != "mix" ]; then
        current_type="$gpu_type"
    else
        case $((i % 3)) in
            0) current_type="nvidia" ;;
            1) current_type="amd" ;;
            2) current_type="intel" ;;
        esac
    fi

    resource_name="${current_type}.com~1gpu"
    curl --header "Content-Type: application/json-patch+json" \
         --request PATCH \
         --data '[{"op":"add","path":"/status/capacity/'${resource_name}'","value":"'${gpus_per_node}'"},
                  {"op":"add","path":"/status/allocatable/'${resource_name}'","value":"'${gpus_per_node}'"}]' \
         http://localhost:8001/api/v1/nodes/${node_name}/status
done

# --------------------------------------------------------------------
# Summary
# --------------------------------------------------------------------
echo "[4/6] Summary of GPU resources in cluster '${cluster_name}':"
echo "-------------------------------------------------------------------------------------------------------------------------------"
printf "%-40s %-20s %-10s %-10s %-30s %-10s\n" "Node" "Resource" "Capacity" "Allocatable" "GPU Product" "Memory (MB)"
echo "-------------------------------------------------------------------------------------------------------------------------------"

for node in "${node_array[@]}"; do
  node_json=$(kubectl get node "$node" -o json)
  for resource in "nvidia.com/gpu" "amd.com/gpu" "intel.com/gpu"; do
    cap=$(echo "$node_json" | jq -r ".status.capacity[\"$resource\"] // empty")
    alloc=$(echo "$node_json" | jq -r ".status.allocatable[\"$resource\"] // empty")
    if [[ -n "$cap" || -n "$alloc" ]]; then
      product=$(echo "$node_json" | jq -r ".metadata.labels[\"${resource}.product\"] // \"-\"")
      memory=$(echo "$node_json" | jq -r ".metadata.labels[\"${resource}.memory\"] // \"-\"")
      printf "%-40s %-20s %-10s %-10s %-30s %-10s\n" "$node" "$resource" "$cap" "$alloc" "$product" "$memory"
    fi
  done
done
echo "-------------------------------------------------------------------------------------------------------------------------------"

echo "[5/6] Cleaning up proxy..."
cleanup

echo "[6/6] Done!"
