#!/bin/bash
export PATH=/home/shjohn/bin:$PATH
export KUBECONFIG=/mnt/c/Users/826657756/.kube/config-pokprod001
export WVA_IMAGE_REPO=ghcr.io/ev-shindin/workload-variant-autoscaler
export WVA_IMAGE_TAG=v0.06
cd /mnt/c/DataD/Work/gpuoptimization/llmd-autoscaler-priv
dos2unix deploy/openshift/install.sh 2>/dev/null || sed -i 's/\r$//' deploy/openshift/install.sh
bash deploy/openshift/install.sh 2>&1 | tee /tmp/pokprod-controller-install.log
