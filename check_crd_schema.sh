#!/bin/bash
# Check if reason and lastUpdate fields exist in the deployed CRD

echo "========== Checking deployed CRD schema =========="
echo ""
echo "Looking for 'desiredOptimizedAlloc' fields in CRD..."
echo ""

kubectl get crd variantautoscalings.llm-d.llm-manager.io -o yaml 2>/dev/null | \
  grep -A 30 "desiredOptimizedAlloc:" | \
  grep -E "(properties:|lastRunTime:|lastUpdate:|numReplicas:|reason:)" || \
  echo "ERROR: CRD not found or desiredOptimizedAlloc section not found"

echo ""
echo "========== Expected fields =========="
echo "Should see:"
echo "  - lastRunTime:"
echo "  - lastUpdate:"
echo "  - numReplicas:"
echo "  - reason:"
echo ""
echo "If 'reason:' or 'lastUpdate:' are MISSING, the CRD needs to be updated!"
