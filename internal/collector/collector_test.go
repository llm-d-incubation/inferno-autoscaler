package controller

import (
	"context"
	"fmt"
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
)

var _ = Describe("Collector", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger.Log = zap.NewNop().Sugar()

		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(appsv1.AddToScheme(scheme)).To(Succeed())
		Expect(llmdVariantAutoscalingV1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	Context("When collecting inventory from K8s", func() {
		It("should collect GPU inventory from multiple nodes", func() {
			// Create nodes with fake GPU labels
			nodes := []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "A100",
							"nvidia.com/gpu.memory":  "40Gi",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("4"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-2",
						Labels: map[string]string{
							"amd.com/gpu.product": "MI300X",
							"amd.com/gpu.memory":  "192Gi",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"amd.com/gpu": resource.MustParse("2"),
						},
					},
				},
			}

			// Create fake client with nodes
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&nodes[0], &nodes[1]).
				Build()

				// Validate results
			inventory, err := CollectInventoryK8S(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(inventory).To(HaveLen(2))

			Expect(inventory["gpu-node-1"]).To(HaveKey("A100"))
			Expect(inventory["gpu-node-1"]["A100"].Count).To(Equal(4))
			Expect(inventory["gpu-node-1"]["A100"].Memory).To(Equal("40Gi"))

			// Check gpu-node-2
			Expect(inventory["gpu-node-2"]).To(HaveKey("MI300X"))
			Expect(inventory["gpu-node-2"]["MI300X"].Count).To(Equal(2))
			Expect(inventory["gpu-node-2"]["MI300X"].Memory).To(Equal("192Gi"))
		})

		It("should handle nodes without GPU labels", func() {
			// Create node without GPU labels
			nodes := []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-1",
						// no GPU labels
					},
					Status: corev1.NodeStatus{
						// no allocatable resources
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "gpu-node-2",
						Labels: map[string]string{
							// no GPU labels
						},
					},
					Status: corev1.NodeStatus{
						// no allocatable resources
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&nodes[0], &nodes[1]).
				Build()

			inventory, err := CollectInventoryK8S(ctx, fakeClient)

			Expect(err).NotTo(HaveOccurred())
			Expect(inventory).To(BeEmpty())
		})

		It("should handle missing GPU capacity", func() {
			// Create node with GPU labels but no allocatable capacity
			nodes := []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "A100",
							"nvidia.com/gpu.memory":  "40Gi",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							// no allocatable capacity
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-2",
						Labels: map[string]string{
							"amd.com/gpu.product": "MI300X",
							"amd.com/gpu.memory":  "192Gi",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							// no allocatable capacity
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&nodes[0], &nodes[1]).
				Build()

			inventory, err := CollectInventoryK8S(ctx, fakeClient)

			Expect(err).NotTo(HaveOccurred())
			Expect(inventory).To(HaveLen(2))
			Expect(inventory["gpu-node-1"]["A100"].Count).To(Equal(0))
			Expect(inventory["gpu-node-1"]["A100"].Memory).To(Equal("40Gi"))
			Expect(inventory["gpu-node-2"]["MI300X"].Count).To(Equal(0))
			Expect(inventory["gpu-node-2"]["MI300X"].Memory).To(Equal("192Gi"))
		})

		It("should handle multiple GPU types on same node", func() {
			// Create node with multiple vendor GPUs
			nodes := []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "A100",
							"nvidia.com/gpu.memory":  "40Gi",
							"intel.com/gpu.product":  "G2",
							"intel.com/gpu.memory":   "96Gi",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("2"),
							"intel.com/gpu":  resource.MustParse("1")},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-2",
						Labels: map[string]string{
							"amd.com/gpu.product": "MI300X",
							"amd.com/gpu.memory":  "192Gi",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"amd.com/gpu": resource.MustParse("2"),
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&nodes[0], &nodes[1]).
				Build()

			inventory, err := CollectInventoryK8S(ctx, fakeClient)

			Expect(err).NotTo(HaveOccurred())
			Expect(inventory).To(HaveLen(2))
			Expect(inventory["gpu-node-1"]).To(HaveLen(2))
			Expect(inventory["gpu-node-1"]["A100"].Count).To(Equal(2))
			Expect(inventory["gpu-node-1"]["G2"].Count).To(Equal(1))
			Expect(inventory["gpu-node-2"]).To(HaveLen(1))
			Expect(inventory["gpu-node-2"]["MI300X"].Count).To(Equal(2))
			Expect(inventory["gpu-node-2"]["MI300X"].Memory).To(Equal("192Gi"))
		})
	})

	Context("When adding metrics to optimization status", func() {
		var (
			mockProm      *mockPromAPI
			deployment    appsv1.Deployment
			va            llmdVariantAutoscalingV1alpha1.VariantAutoscaling
			name          string
			modelID       string
			testNamespace string
			accCost       float64
		)

		BeforeEach(func() {
			mockProm = &mockPromAPI{
				queryResults: make(map[string]model.Value),
				queryErrors:  make(map[string]error),
			}

			name = "test"
			modelID = "default/default"
			testNamespace = "default"
			accCost = 40.0 // sample accelerator cost

			deployment = appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: func() *int32 { r := int32(2); return &r }(),
				},
			}

			va = llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
					Labels: map[string]string{
						"inference.optimization/acceleratorName": "A100",
					},
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID: modelID,
				},
			}
		})

		It("should collect metrics successfully", func() {
			// Setup mock responses
			arrivalQuery := `sum(rate(vllm:request_success_total{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m])) * 60`
			tokenQuery := `sum(rate(vllm:request_generation_tokens_sum{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))/sum(rate(vllm:request_generation_tokens_count{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))`
			waitQuery := `sum(rate(vllm:request_queue_time_seconds_sum{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))/sum(rate(vllm:request_queue_time_seconds_count{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))`
			itlQuery := `sum(rate(vllm:time_per_output_token_seconds_sum{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))/sum(rate(vllm:time_per_output_token_seconds_count{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))`

			mockProm.queryResults[arrivalQuery] = model.Vector{
				&model.Sample{Value: model.SampleValue(10.5)}, // 10.5 requests/min
			}
			mockProm.queryResults[tokenQuery] = model.Vector{
				&model.Sample{Value: model.SampleValue(150.0)}, // 150 tokens per request
			}
			mockProm.queryResults[waitQuery] = model.Vector{
				&model.Sample{Value: model.SampleValue(0.5)}, // 0.5 seconds
			}
			mockProm.queryResults[itlQuery] = model.Vector{
				&model.Sample{Value: model.SampleValue(0.05)}, // 0.05 seconds
			}

			allocation, err := AddMetricsToOptStatus(ctx, &va, deployment, accCost, mockProm)

			Expect(err).NotTo(HaveOccurred())
			Expect(allocation.Accelerator).To(Equal("A100"))
			Expect(allocation.NumReplicas).To(Equal(2))
			Expect(allocation.MaxBatch).To(Equal(256))
			Expect(allocation.VariantCost).To(Equal("80.00"))           // 2 replicas * 40.0 acc cost
			Expect(allocation.TTFTAverage).To(Equal("500.00"))          // 0.5 * 1000 ms
			Expect(allocation.ITLAverage).To(Equal("50.00"))            // 0.05 * 1000 ms
			Expect(allocation.Load.ArrivalRate).To(Equal("10.50"))      // req per min
			Expect(allocation.Load.AvgOutputTokens).To(Equal("150.00")) // tokens per req
		})

		It("should handle missing accelerator label", func() {
			// Remove accelerator label
			delete(va.Labels, "inference.optimization/acceleratorName")

			// Setup minimal mock responses
			arrivalQuery := `sum(rate(vllm:request_success_total{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m])) * 60`
			tokenQuery := `sum(rate(vllm:request_generation_tokens_sum{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))/sum(rate(vllm:request_generation_tokens_count{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m]))`

			mockProm.queryResults[arrivalQuery] = model.Vector{
				&model.Sample{Value: model.SampleValue(5.0)},
			}
			mockProm.queryResults[tokenQuery] = model.Vector{
				&model.Sample{Value: model.SampleValue(100.0)},
			}

			allocation, err := AddMetricsToOptStatus(ctx, &va, deployment, accCost, mockProm)

			Expect(err).NotTo(HaveOccurred())
			Expect(allocation.Accelerator).To(Equal("")) // Empty due to deleted accName label
		})

		It("should handle Prometheus query errors", func() {
			// Setup error for arrival query
			arrivalQuery := `sum(rate(vllm:request_success_total{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m])) * 60`
			mockProm.queryErrors[arrivalQuery] = fmt.Errorf("prometheus connection failed")

			allocation, err := AddMetricsToOptStatus(ctx, &va, deployment, accCost, mockProm)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("prometheus connection failed"))
			Expect(allocation).To(Equal(llmdVariantAutoscalingV1alpha1.Allocation{})) // Expect empty allocation on error
		})

		It("should handle empty metric results gracefully", func() {
			// Setup empty responses (no data points)
			arrivalQuery := `sum(rate(vllm:request_success_total{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m])) * 60`
			tokenQuery := `delta(vllm:tokens_total{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m])/delta(vllm:request_success_total{model_name="` + modelID + `",namespace="` + testNamespace + `"}[1m])`

			// Empty vectors (no data)
			mockProm.queryResults[arrivalQuery] = model.Vector{}
			mockProm.queryResults[tokenQuery] = model.Vector{}

			allocation, err := AddMetricsToOptStatus(ctx, &va, deployment, accCost, mockProm)

			Expect(err).NotTo(HaveOccurred())
			Expect(allocation.ITLAverage).To(Equal("0.00"))
			Expect(allocation.TTFTAverage).To(Equal("0.00"))
			Expect(allocation.Load.ArrivalRate).To(Equal("0.00"))
			Expect(allocation.Load.AvgInputTokens).To(Equal("0.00"))
			Expect(allocation.Load.AvgOutputTokens).To(Equal("0.00"))
		})
	})

	Context("When testing FixValue func", func() {
		It("should fix NaN values", func() {
			val := math.NaN()
			FixValue(&val)
			Expect(val).To(Equal(0.0))
		})

		It("should fix positive infinity", func() {
			val := math.Inf(1)
			FixValue(&val)
			Expect(val).To(Equal(0.0))
		})

		It("should fix negative infinity", func() {
			val := math.Inf(-1)
			FixValue(&val)
			Expect(val).To(Equal(0.0))
		})

		It("should not modify normal values", func() {
			val := 42.5
			FixValue(&val)
			Expect(val).To(Equal(42.5))
		})

		It("should not modify zero", func() {
			val := 0.0
			FixValue(&val)
			Expect(val).To(Equal(0.0))
		})

		It("should not modify negative values", func() {
			val := -15.3
			FixValue(&val)
			Expect(val).To(Equal(-15.3))
		})
	})

	Context("When testing AcceleratorModelInfo struct", func() {
		It("should create AcceleratorModelInfo correctly", func() {
			info := AcceleratorModelInfo{
				Count:  8,
				Memory: "80Gi",
			}

			Expect(info.Count).To(Equal(8))
			Expect(info.Memory).To(Equal("80Gi"))
		})
	})

	Context("When testing MetricKV struct", func() {
		It("should create MetricKV correctly", func() {
			metric := MetricKV{
				Name: "test_metric",
				Labels: map[string]string{
					"model": "test-model",
					"gpu":   "A100",
				},
				Value: 123.45,
			}

			Expect(metric.Name).To(Equal("test_metric"))
			Expect(metric.Labels).To(HaveKeyWithValue("model", "test-model"))
			Expect(metric.Labels).To(HaveKeyWithValue("gpu", "A100"))
			Expect(metric.Value).To(Equal(123.45))
		})
	})

	Context("When testing vendor list", func() {
		It("should have expected GPU vendors", func() {
			expectedVendors := []string{
				"nvidia.com",
				"amd.com",
				"intel.com",
			}

			Expect(vendors).To(ConsistOf(expectedVendors))
		})
	})
})
