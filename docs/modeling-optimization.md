# Workload Variant Autoscaler: Under the hood

The function of the Workload Variant Autoscaler (WVA) is to decide on the number of replicas for each variant, in response to changes in the workload requests rate and length (number of tokens).
WVA is a global autoscaler, as opposed to a set of independent, local autoscalers, each performing scale-up or scale-down in units of one replica, in response to a configured threshold-based metric.
As such, WVA considers all variants in the system, holistically.
It adjusts the values of the number of replicas based on a number of factors, including, but not limited to, the number of available accelerators, load statistics, model performance profiles, target SLOs, and workload priorities (criticality).
WVA uses modeling, benchmarking, and optimization to find the best possible solution for all variants in the system.

## Definitions and assumptions

- An **accelerator** is a unit of allocation of (GPU) devices, of a given type and multiplicity, e.g. 2xH100 is an accelarator consisting of two H100 GPUs, in order to satisfy model memory constraints and/or performance.

- An **accelerator configuration** (**topology**, for short) consists of one or more accelerators in parallel (tensor or pipeline parallelism) assigned to a model server.

- A **variant** is a collection of model servers (variant instances or **replicas**) serving a given model, using the same accelerator configuration.

- A **model-topology performance profile** captures performance characteristics when serving a given model on a given accelerator topology. The profile includes:

  1. functional description of the token generation time (TPOT) as a function of batch size, and
  2. characterization of conditions under which the server is saturated.

    The performance profile may be generated through offline benchmarking and/or dynamically updated based on online observations.

- The **model SLOs** define target values for two metrics:

  1. *TTFT*: The TTFT component includes request queueing time as well as waiting and performing prefill processing. Since the latter is a function of the number of input tokens in the request, we use the total request queueing time as a surrogate, request-independent measure to TTFT.
  2. *TPOT*: This is simply the decode time to generate an output token. It is subject to elongation due to congestion, resulting from batching requests, injection of prefill processing during a long decode cycle, and factors related to KV caching and potential memory swapping.

- **Workload priority** (aka **criticality**) is an indicator of the importance of requests of a particular application (workload). It may serve different functions depending on the component that is handling the workload. For an admission controller, it may be used to decide on which stream of requests is more likely to be dropped. For a request scheduler, it may influence the position of a request in the queue and/or when dispatching a request. And, for WVA, it is used to decide on the assignment of accelerators to variants serving particular workloads when the total resources are tight, i.e. cannot accommodate the SLOs for all models.

## Modeling

The model analyzer maintains an analytical performance model for each variant in the system. Such a performance model captures the statistical behavior of requests as they pass through a server, including queueing and processing times, as a function load characteristics, such as request rates and sizes (input and output tokens), and server characteristics such as GPU type and configuration (P/D disaggregation, chunked prefill, etc). The performance model may be based on queueing theory, machine learning techniques, or other mechanisms.

The purpose of using a performance model is twofold.

1. Performance evaluation: Estimate performance metrics such as waiting time, TTFT, ITL, and TPOT, as a function of a given load and server characteristics.

2. Target sizing: Determine load and/or server characteristics in order to attain target values of performance metrics.
The former is used to estimate performance given the current and/or predicted/anticipated environment. Whereas, the latter is mainly used by the Optimizer to assess maximum request rate to guarantee given SLOs, as well as the impact of a choice of a particular GPU type.

Typically, analytical performance models have their own internal parameters. For example, the base and slope of the linear relationship between ITL and batch size ([explained below in more detail](#deriving-performance-parameters-through-linear-fit)), are parameters of the model. In this case, the determination of such parameters may be achieved through offline benchmarking and/or online through observations and tuning (dynamic adjustment of parameter values to match observations).

The other relevant performance parameter is an upper bound on the batch size, given a particular average number of tokens per request, beyond which preformance degrades severely.

## Benchmarking methodology

To understand and characterize the performance of an LLM model on an accelerator, we conducted a series of benchmarking experiments. The goal was to establish a clear relationship between key performance metrics, specifically inter-token latency (ITL) and batch size (number of requests concurrently processed in a forward pass of the model).

### Experimental setup and data collection

Our experiments were run on an on-premise cluster and were executed on a variety of hardware platforms, including NVIDIA L40S, L4, H100, A100, and AMD MI300X, Intel Gaudi3, using vLLM v0 inference engine.

The models under test included a wide range of architectures and sizes, such as llama2-7b, llama3-8b, granite-20b, mixtral-8x7b, and many others. Tests at various numerical precisions, specifically fp16, w4a16, and fp8, were performed to evaluate the performance trade-offs.

For each unique combination of model, accelerator, and precision, the batch size (`bb`) was systematically varied and a rich dataset was collected. For each experiment run, the following data was recorded.

- `mm`: The specific model name.
- `hw`: The hardware accelerator used for the experiment.
- `prec`: The numerical precision of the model.
- `bb`: The batch size, the independent variable in our analysis.
- `itl`: The measured inter-token latency in milliseconds (ms).
- `thp`: The resulting throughput in tokens per second.
- `dp`: data parallel size (# of parallel instances; usually 1)
- `tp`: tensor parallel size with values including 1, 2, 4, and 8.

### Deriving performance parameters through linear fit

A consistent pattern emerged from our benchmarking data: for a given model and accelerator, the inter-token latency ('itl') exhibited a strong linear relationship with the batch size ('bb').
This behavior has been observed, experimentally, by many researchers [^Agrawal2024] [^Griggs2024] [^Yang2024] [^Yuan2024] [^Zhu2025].
To quantify this relationship, we performed a linear regression fit for each unique model-accelerator-precision combination. The relationship can be described by the following linear equation:

$$ITL = \alpha + \beta \times bb,$$

where ITL is the inter-token latency in milliseconds and `bb` is the batch size.
The linear fit parameter $\alpha$ is the y-intercept, representing the baseline inter-token latency at a batch size close to zero. This can be interpreted as the fixed overhead of a single token generation, independent of the batching process itself.
Parameter $\beta$ is the slope of the line, representing the increase in ITL for each unit increase in batch size. This parameter captures how the latency scales with the workload.

By fitting our benchmark data to this linear model, we derived specific values for $\alpha$ and $\beta$.

We note that such linear dependency only on the batch size discounts the impact of sequence length. Nevertheless, a first-order approximation of ITL using a simple analytical formula $\alpha + \beta \times bb$ is still helpful in designing a performance-aware system or in making initial deployment decisions.

Finally, we observe that with respect to a physical system, these parameters capture the model dimension, the data/tensor parallelism, and the theoretical peak flops of the accelerator.
As such, one can estimate these parameters using either a model tuner that uses an Extended Kalman Filter in the backend and learns the model-accelerator characteristics, or by simply substituting the knowledge of the physical system into a FLOPS compute formula [^Casson2023].

## Optimization

The optimizer considers all variants in the system and the availability of accelerators in the cluster.
Its objective is to mimize total cost, while satisfying the SLOs for all variants.
In case there are not enough accelerators to satisfy all SLOs, the variant with higher priority will be satisfied first.
The optimizer uses the model analyzer to estimate the number of replicas needed for each variant to satisfy its SLOs, given the observed load statistics.

## Roadmap

Planned extensions to WVA.

- **Decision variables:** In addition to the number of replicas, WVA decides on

    1. the accelerators assigned to a given variant, in a heterogeneous environment, and
    2. the maximum request concurrency for a variant (This could be achieved through setting the maximum batch size of the vllm server or configuring a maximum concurrency parameter for the request scheduler to impose).

- **Workload criticality:** Consider tradeoffs among variants with different criticality, in case the SLOs cannot be met with the given load. For example, dropping requests to maintain SLOs.

- **P/D disaggregation:** Extend queueing model to handle P and D only, as well as P/D processing.

- **Flexible objective function:** Extend the objective function beyond minimizing cost.

- **Collaboration with scheduler:** There are cases where WVA and the scheduler could collaboratively help each other, through passing hints.

- **Batch workload:** Extend WVA to not only handle interactive workload, but also batch workloads, and combinations of both workloads.

## References

[^Agrawal2024]: Agrawal, Amey, et al. "[Taming Throughput-Latency tradeoff in LLM inference with Sarathi-Serve.](https://www.usenix.org/system/files/osdi24-agrawal.pdf)" 18th USENIX Symposium on Operating Systems Design and Implementation (OSDI 24). 2024.

[^Casson2023]: Adam Casson "[Transformer FLOPs](https://www.adamcasson.com/posts/transformer-flops)"

[^Griggs2024]: Griggs, Tyler, et al. "[M\'elange: Cost efficient large language model serving by exploiting gpu heterogeneity.](https://arxiv.org/pdf/2404.14527)" arXiv preprint arXiv:2404.14527 (2024).

[^Yang2024]: Yang, Yuqing, Lei Jiao, and Yuedong Xu. "[A queueing theoretic perspective on low-latency llm inference with variable token length.](https://ieeexplore.ieee.org/abstract/document/10778367/)" 2024 22nd International Symposium on Modeling and Optimization in Mobile, Ad Hoc, and Wireless Networks (WiOpt). IEEE, 2024.

[^Yuan2024]: Yuan, Zhihang, et al. "[LLM inference unveiled: Survey and roofline model insights.](https://arxiv.org/abs/2402.16363)" arXiv preprint arXiv:2402.16363 (2024).

[^Zhu2025]: Zhu, Kan, et al. "[PolyServe: Efficient Multi-SLO Serving at Scale.](https://arxiv.org/pdf/2507.17769?)" arXiv preprint arXiv:2507.17769 (2025).
