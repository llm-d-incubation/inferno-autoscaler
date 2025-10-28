// Package constants provides centralized constant definitions for the autoscaler.
package constants

// VLLM Input Metrics
// These metric names are used to query VLLM (vLLM inference engine) metrics from Prometheus.
// The metrics are emitted by VLLM servers and consumed by the collector to make scaling decisions.
const (
	// VLLMRequestSuccessTotal tracks the total number of successful requests.
	// Used to calculate arrival rate.
	VLLMRequestSuccessTotal = "vllm:request_success_total"

	// VLLMRequestGenerationTokensSum tracks the sum of generated tokens across all requests.
	// Used with VLLMRequestGenerationTokensCount to calculate average output tokens.
	VLLMRequestGenerationTokensSum = "vllm:request_generation_tokens_sum"

	// VLLMRequestGenerationTokensCount tracks the count of requests for token generation.
	// Used with VLLMRequestGenerationTokensSum to calculate average output tokens.
	VLLMRequestGenerationTokensCount = "vllm:request_generation_tokens_count"

	// VLLMRequestQueueTimeSecondsSum tracks the sum of queue time across all requests.
	// Used with VLLMRequestQueueTimeSecondsCount to calculate TTFT (Time To First Token).
	VLLMRequestQueueTimeSecondsSum = "vllm:request_queue_time_seconds_sum"

	// VLLMRequestQueueTimeSecondsCount tracks the count of requests for queue time.
	// Used with VLLMRequestQueueTimeSecondsSum to calculate TTFT (Time To First Token).
	VLLMRequestQueueTimeSecondsCount = "vllm:request_queue_time_seconds_count"

	// VLLMTimePerOutputTokenSecondsSum tracks the sum of time per output token across all requests.
	// Used with VLLMTimePerOutputTokenSecondsCount to calculate ITL (Inter-Token Latency).
	VLLMTimePerOutputTokenSecondsSum = "vllm:time_per_output_token_seconds_sum"

	// VLLMTimePerOutputTokenSecondsCount tracks the count of requests for time per output token.
	// Used with VLLMTimePerOutputTokenSecondsSum to calculate ITL (Inter-Token Latency).
	VLLMTimePerOutputTokenSecondsCount = "vllm:time_per_output_token_seconds_count"
)

// Inferno Output Metrics
// These metric names are used to emit Inferno autoscaler metrics to Prometheus.
// The metrics expose scaling decisions and current state for monitoring and alerting.
const (
	// InfernoReplicaScalingTotal is a counter that tracks the total number of scaling operations.
	// Labels: variant_name, namespace, direction (up/down), reason, accelerator_type
	InfernoReplicaScalingTotal = "inferno_replica_scaling_total"

	// InfernoDesiredReplicas is a gauge that tracks the desired number of replicas.
	// Labels: variant_name, namespace, accelerator_type
	InfernoDesiredReplicas = "inferno_desired_replicas"

	// InfernoCurrentReplicas is a gauge that tracks the current number of replicas.
	// Labels: variant_name, namespace, accelerator_type
	InfernoCurrentReplicas = "inferno_current_replicas"

	// InfernoDesiredRatio is a gauge that tracks the ratio of desired to current replicas.
	// Labels: variant_name, namespace, accelerator_type
	InfernoDesiredRatio = "inferno_desired_ratio"
)

// Metric Label Names
// Common label names used across metrics for consistency.
const (
	LabelModelName       = "model_name"
	LabelNamespace       = "namespace"
	LabelVariantName     = "variant_name"
	LabelVariantID       = "variant_id"
	LabelDirection       = "direction"
	LabelReason          = "reason"
	LabelAcceleratorType = "accelerator_type"
)
