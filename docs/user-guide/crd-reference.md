# API Reference

## Packages
- [llmd.ai/v1alpha1](#llmdaiv1alpha1)


## llmd.ai/v1alpha1

Package v1alpha1 contains API Schema definitions for the llmd v1alpha1 API group.

### Resource Types
- [VariantAutoscaling](#variantautoscaling)
- [VariantAutoscalingList](#variantautoscalinglist)



#### ActuationStatus



ActuationStatus provides details about the actuation process and its current status.



_Appears in:_
- [VariantAutoscalingStatus](#variantautoscalingstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `applied` _boolean_ | Applied indicates whether the actuation was successfully applied. |  |  |


#### Allocation



Allocation describes the current resource allocation for this variant.



_Appears in:_
- [VariantAutoscalingStatus](#variantautoscalingstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `variantID` _string_ | VariantID identifies this variant.<br />Format: \{modelID\}-\{accelerator\}-\{acceleratorCount\} |  | MinLength: 1 <br /> |
| `accelerator` _string_ | Accelerator is the type of accelerator currently allocated. |  | MinLength: 1 <br /> |
| `numReplicas` _integer_ | NumReplicas is the number of replicas currently allocated. |  | Minimum: 0 <br /> |
| `maxBatch` _integer_ | MaxBatch is the maximum batch size currently allocated. |  | Minimum: 0 <br /> |
| `variantCost` _string_ | VariantCost is the cost associated with this variant allocation. |  | Pattern: `^\d+(\.\d+)?$` <br /> |


#### ConfigMapKeyRef



ConfigMapKeyRef references a specific key within a ConfigMap.



_Appears in:_
- [VariantAutoscalingSpec](#variantautoscalingspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the ConfigMap. |  | MinLength: 1 <br /> |
| `key` _string_ | Key is the key within the ConfigMap. |  | MinLength: 1 <br /> |


#### OptimizedAlloc



OptimizedAlloc describes the target optimized allocation for a model variant.



_Appears in:_
- [VariantAutoscalingStatus](#variantautoscalingstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastRunTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#time-v1-meta)_ | LastRunTime is the timestamp of the last optimization run. |  |  |
| `variantID` _string_ | VariantID identifies which variant this optimized allocation is for.<br />Format: \{modelID\}-\{accelerator\}-\{acceleratorCount\} |  | MinLength: 1 <br /> |
| `accelerator` _string_ | Accelerator is the type of accelerator for the optimized allocation. |  | MinLength: 2 <br /> |
| `numReplicas` _integer_ | NumReplicas is the number of replicas for the optimized allocation. |  | Minimum: 0 <br /> |


#### PerfParms



PerfParms contains performance parameters for the variant.



_Appears in:_
- [VariantProfile](#variantprofile)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `decodeParms` _object (keys:string, values:string)_ | DecodeParms contains parameters for the decode phase (ITL calculation).<br />Expected keys: "alpha", "beta" for equation: itl = alpha + beta * maxBatchSize |  | MinProperties: 1 <br /> |
| `prefillParms` _object (keys:string, values:string)_ | PrefillParms contains parameters for the prefill phase (TTFT calculation).<br />Expected keys: "gamma", "delta" for equation: ttft = gamma + delta * tokens * maxBatchSize |  | MinProperties: 1 <br /> |


#### VariantAutoscaling



VariantAutoscaling is the Schema for the variantautoscalings API.
It represents the autoscaling configuration and status for a model variant.



_Appears in:_
- [VariantAutoscalingList](#variantautoscalinglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `llmd.ai/v1alpha1` | | |
| `kind` _string_ | `VariantAutoscaling` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[VariantAutoscalingSpec](#variantautoscalingspec)_ | Spec defines the desired state for autoscaling the model variant. |  |  |
| `status` _[VariantAutoscalingStatus](#variantautoscalingstatus)_ | Status represents the current status of autoscaling for the model variant. |  |  |


#### VariantAutoscalingList



VariantAutoscalingList contains a list of VariantAutoscaling resources.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `llmd.ai/v1alpha1` | | |
| `kind` _string_ | `VariantAutoscalingList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[VariantAutoscaling](#variantautoscaling) array_ | Items is the list of VariantAutoscaling resources. |  |  |


#### VariantAutoscalingSpec



VariantAutoscalingSpec defines the desired state for autoscaling a model variant.



_Appears in:_
- [VariantAutoscaling](#variantautoscaling)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `modelID` _string_ | ModelID specifies the unique identifier of the model to be autoscaled. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `variantID` _string_ | VariantID uniquely identifies this variant (model + accelerator + acceleratorCount combination).<br />This is a business identifier that may contain slashes, dots, and mixed case.<br />Format: \{modelID\}-\{accelerator\}-\{acceleratorCount\}<br />Example: "meta/llama-3.1-8b-A100-4"<br />Note: VariantID (variant_id) is distinct from the VariantAutoscaling resource name (variant_name):<br />  - variant_id (this field): Business identifier, may contain non-K8s-compliant characters<br />  - variant_name (resource.Name): Kubernetes resource name (DNS-1123 compliant)<br />Both identifiers are exposed as Prometheus labels for flexible querying:<br />  - Use variant_name to query by Kubernetes resource (typically matches Deployment name)<br />  - Use variant_id to query by business identifier (model/variant naming) |  | MinLength: 1 <br />Pattern: `^.+-[A-Za-z0-9]+-[1-9][0-9]*$` <br />Required: \{\} <br /> |
| `accelerator` _string_ | Accelerator specifies the accelerator type for this variant (e.g., "A100", "L40S"). |  | MinLength: 1 <br />Required: \{\} <br /> |
| `acceleratorCount` _integer_ | AcceleratorCount specifies the number of accelerator units per replica. |  | Minimum: 1 <br />Required: \{\} <br /> |
| `sloClassRef` _[ConfigMapKeyRef](#configmapkeyref)_ | SLOClassRef references the ConfigMap key containing Service Level Objective (SLO) configuration. |  | Required: \{\} <br /> |
| `variantProfile` _[VariantProfile](#variantprofile)_ | VariantProfile provides performance characteristics for this variant. |  | Required: \{\} <br /> |


#### VariantAutoscalingStatus



VariantAutoscalingStatus represents the current status of autoscaling for this specific variant.
Since each VariantAutoscaling CR represents a single variant, status contains singular allocation
fields rather than arrays.



_Appears in:_
- [VariantAutoscaling](#variantautoscaling)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `currentAlloc` _[Allocation](#allocation)_ | CurrentAlloc specifies the current resource allocation for this variant. |  |  |
| `desiredOptimizedAlloc` _[OptimizedAlloc](#optimizedalloc)_ | DesiredOptimizedAlloc indicates the target optimized allocation based on autoscaling logic. |  |  |
| `actuation` _[ActuationStatus](#actuationstatus)_ | Actuation provides details about the actuation process and its current status. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#condition-v1-meta) array_ | Conditions represent the latest available observations of the VariantAutoscaling's state |  |  |


#### VariantProfile



VariantProfile provides performance characteristics for a specific variant.



_Appears in:_
- [VariantAutoscalingSpec](#variantautoscalingspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `perfParms` _[PerfParms](#perfparms)_ | PerfParms specifies the prefill and decode parameters for TTFT and ITL models. |  | Required: \{\} <br /> |
| `maxBatchSize` _integer_ | MaxBatchSize is the maximum batch size supported by this variant. |  | Minimum: 1 <br />Required: \{\} <br /> |


