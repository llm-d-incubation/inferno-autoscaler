package controller

import (
	"fmt"
	"strconv"
	"time"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/constants"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	infernoConfig "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/config"
	tune "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/tuner"
	"gonum.org/v1/gonum/mat"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// build config data from defaults, init state and slos
func BuildTunerConfig(
	state []float64,
	covMatrix []float64,
	slos []float64,
) (*tune.TunerConfigData, error) {

	if len(state) == 0 || len(slos) == 0 {
		return nil, fmt.Errorf("initState and slos must be non-empty")
	}
	return &tune.TunerConfigData{
		FilterData: getDefaultFilterData(),
		ModelData: tune.TunerModelData{
			InitState:            state,
			InitCovarianceMatrix: covMatrix,
			PercentChange:        getDefaultPercentChange(),
			BoundedState:         true,
			MinState:             getFactoredState(state, constants.DefaultMinStateFactor),
			MaxState:             getFactoredState(state, constants.DefaultMaxStateFactor),
			ExpectedObservations: slos,
		},
	}, nil
}

func getDefaultFilterData() tune.FilterData {
	return tune.FilterData{
		GammaFactor: constants.DefaultGammaFactor,
		ErrorLevel:  constants.DefaultErrorLevel,
		TPercentile: constants.DefaultTPercentile,
	}
}

func getDefaultPercentChange() []float64 {
	return []float64{
		constants.DefaultPercentChange, // alpha variance
		constants.DefaultPercentChange, // beta variance
		constants.DefaultPercentChange, // gamma variance
		constants.DefaultPercentChange, // delta variance
	}
}

// multiply each element in initState by multiplier and returns the new slice
func getFactoredState(initState []float64, multiplier float64) []float64 {
	multipliedNumbers := make([]float64, len(initState))
	for i, num := range initState {
		multipliedNumbers[i] = num * multiplier
	}
	return multipliedNumbers
}

// convertAllocToEnvironment converts WVA CurrentAlloc to model-tuner Environment.
// This is the adapter between the WVA collector and the Kalman filter tuner.
func convertAllocToEnvironment(alloc infernoConfig.AllocationData) *tune.Environment {
	// first get the request rate per min per replica
	var ratePerReplica float32
	if alloc.NumReplicas > 0 {
		ratePerReplica = alloc.Load.ArrivalRate / float32(alloc.NumReplicas)
	}
	return &tune.Environment{
		Lambda:        ratePerReplica,
		AvgOutputToks: alloc.Load.AvgOutTokens,
		AvgInputToks:  alloc.Load.AvgInTokens,
		MaxBatchSize:  alloc.MaxBatch,
		AvgTTFT:       alloc.TTFTAverage,
		AvgITL:        alloc.ITLAverage,
		NumReplicas:   alloc.NumReplicas,
	}
}

// get state and covariance matrix from VA status (if exist), otherwise return only the state params from VA spec
func getStateAndCovariance(
	va *llmdVariantAutoscalingV1alpha1.VariantAutoscaling,
	systemData *infernoConfig.SystemData,
	server *infernoConfig.ServerSpec,
) (state, covMatrix []float64, err error) {
	// check if VA has tuned results in status (has been tuned before)
	if hasTunedResults(va) {
		state, covMatrix, err = extractStateAndCovarianceFromVAStatus(va)
		if err == nil {
			logger.Log.Debugf("Using state vals from VA status to tune variant %s: alpha= %.6f, beta= %.6f, gamma= %.6f, delta= %.6f",
				va.Name,
				state[constants.StateIndexAlpha],
				state[constants.StateIndexBeta],
				state[constants.StateIndexGamma],
				state[constants.StateIndexDelta])
			return state, covMatrix, nil
		}
		logger.Log.Warnf("Failed to extract tuned state from VA status, falling back to spec for variant %s: %v",
			va.Name,
			err)
	}

	// in case of first time tuning or error in extracting status, fall back to spec values
	state, err = findStateInSystemData(systemData, server.Model, server.CurrentAlloc.Accelerator)
	if err != nil {
		return nil, nil, err
	}

	logger.Log.Debugf("Using initial state from spec for variant %s: alpha= %.6f, beta= %.6f, gamma= %.6f, delta= %.6f",
		va.Name,
		state[constants.StateIndexAlpha],
		state[constants.StateIndexBeta],
		state[constants.StateIndexGamma],
		state[constants.StateIndexDelta])

	return state, nil, nil
}

// hasTunedParams checks if VA status contains tuned parameters.
func hasTunedResults(va *llmdVariantAutoscalingV1alpha1.VariantAutoscaling) bool {
	return len(va.Status.TunerPerfData.PerfParms.DecodeParms) > 0 &&
		len(va.Status.TunerPerfData.PerfParms.PrefillParms) > 0 &&
		len(va.Status.TunerPerfData.CovarianceMatrix) > 0
}

// extracts the state params (alpha, beta, gamma , delta) and the covariance matrix from the VA status
func extractStateAndCovarianceFromVAStatus(va *llmdVariantAutoscalingV1alpha1.VariantAutoscaling) (state, covMatrix []float64, err error) {
	state, err = extractStateFromVAStatus(va)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract tuned state from VA status: %w", err)
	}

	covMatrix, err = extractCovarianceFromVAStatus(va)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract covariance matrix from VA status: %w", err)
	}

	return state, covMatrix, nil
}

func extractCovarianceFromVAStatus(va *llmdVariantAutoscalingV1alpha1.VariantAutoscaling) ([]float64, error) {
	matStatus := va.Status.TunerPerfData.CovarianceMatrix
	rows := len(matStatus)
	cols := len(matStatus[0])
	if rows != cols || rows != 4 {
		return nil, fmt.Errorf("invalid covariance matrix dimensions: expected 4 rows and 4 cols, got %d x %d", rows, cols)
	}

	// Create a flat slice to hold the float64 data
	data := make([]float64, rows*cols)

	// Populate the data slice by parsing strings
	for r := 0; r < rows; r++ {
		if len(matStatus[r]) != cols {
			return nil, fmt.Errorf("row %d has inconsistent column count", r)
		}
		for c := 0; c < cols; c++ {
			val, err := strconv.ParseFloat(matStatus[r][c], 32)
			if err != nil {
				return nil, fmt.Errorf("error parsing string '%s' to float64: %v", matStatus[r][c], err)
			}
			data[r*cols+c] = val
		}
	}
	return data, nil
}

func extractStateFromVAStatus(va *llmdVariantAutoscalingV1alpha1.VariantAutoscaling) ([]float64, error) {
	// extract decode model (itl) parameters
	decodeParms := va.Status.TunerPerfData.PerfParms.DecodeParms
	if len(decodeParms) != 2 {
		return nil, fmt.Errorf("length of tuned decode parms in VA status should be 2")
	}
	alpha, err := strconv.ParseFloat(decodeParms["alpha"], 32)
	if err != nil {
		return nil, err
	}
	beta, err := strconv.ParseFloat(decodeParms["beta"], 32)
	if err != nil {
		return nil, err
	}

	// extract prefill model (ttft) parameters
	prefillParms := va.Status.TunerPerfData.PerfParms.PrefillParms
	if len(prefillParms) != 2 {
		return nil, fmt.Errorf("length of prefillParms should be 2")
	}
	gamma, err := strconv.ParseFloat(prefillParms["gamma"], 32)
	if err != nil {
		return nil, err
	}
	delta, err := strconv.ParseFloat(prefillParms["delta"], 32)
	if err != nil {
		return nil, err
	}

	return []float64{alpha, beta, gamma, delta}, nil
}

func findStateInSystemData(
	systemData *infernoConfig.SystemData,
	modelName string,
	acceleratorName string,
) ([]float64, error) {

	for _, perfData := range systemData.Spec.Models.PerfData {
		if perfData.Name == modelName && perfData.Acc == acceleratorName {
			alpha := float64(perfData.DecodeParms.Alpha)
			beta := float64(perfData.DecodeParms.Beta)
			gamma := float64(perfData.PrefillParms.Gamma)
			delta := float64(perfData.PrefillParms.Delta)

			// Validate all parameters are positive
			if alpha <= 0 || beta <= 0 || gamma <= 0 || delta <= 0 {
				return nil, fmt.Errorf("invalid parameters: alpha=%f, beta=%f, gamma=%f, delta=%f (must be positive)",
					alpha, beta, gamma, delta)
			}

			return []float64{alpha, beta, gamma, delta}, nil
		}
	}
	return nil, fmt.Errorf("model %q with accelerator %q not found in system data", modelName, acceleratorName)
}

func findSLOInSystemData(
	systemData *infernoConfig.SystemData,
	modelName string,
	serviceClassName string,
) ([]float64, error) {
	var svcSpecs *infernoConfig.ServiceClassSpec
	for _, spec := range systemData.Spec.ServiceClasses.Spec {
		if spec.Name == serviceClassName {
			svcSpecs = &spec
			break
		}
	}

	if svcSpecs == nil {
		return nil, fmt.Errorf("service class %q not found in system data", serviceClassName)
	}

	for _, modelTarget := range svcSpecs.ModelTargets {
		if modelTarget.Model == modelName {
			sloTTFT := float64(modelTarget.SLO_TTFT)
			sloITL := float64(modelTarget.SLO_ITL)

			// Validate SLOs are positive
			if sloTTFT <= 0 || sloITL <= 0 {
				return nil, fmt.Errorf("invalid SLOs for model %q: TTFT=%f, ITL=%f (must be positive)",
					modelName, sloTTFT, sloITL)
			}

			return []float64{sloTTFT, sloITL}, nil
		}
	}
	return nil, fmt.Errorf("model %q not found in service class %q", modelName, serviceClassName)
}

// updates queueing model parameters in the system data
func updateModelPerfDataInSystemData(systemData *infernoConfig.SystemData, modelName, accName string, tunedResults *tune.TunedResults) error {
	for i := range systemData.Spec.Models.PerfData {
		perfData := &systemData.Spec.Models.PerfData[i]
		if perfData.Name == modelName && perfData.Acc == accName {
			perfData.DecodeParms.Alpha = float32(tunedResults.ServiceParms.Decode.Alpha)
			perfData.DecodeParms.Beta = float32(tunedResults.ServiceParms.Decode.Beta)
			perfData.PrefillParms.Gamma = float32(tunedResults.ServiceParms.Prefill.Gamma)
			perfData.PrefillParms.Delta = float32(tunedResults.ServiceParms.Prefill.Delta)

			logger.Log.Debugf("Model tuner results: model=%s, accelerator=%s, alpha=%.6f, beta=%.6f, gamma=%.6f, delta=%.6f, NIS=%.2f",
				modelName,
				accName,
				perfData.DecodeParms.Alpha,
				perfData.DecodeParms.Beta,
				perfData.PrefillParms.Gamma,
				perfData.PrefillParms.Delta,
				tunedResults.NIS,
			)

			return nil
		}
	}
	return fmt.Errorf("model %q with accelerator %q not found in system data", modelName, accName)
}

// updates VA status with tuner results
func updateVAStatusWithTunedParams(
	va *llmdVariantAutoscalingV1alpha1.VariantAutoscaling,
	model string,
	accelerator string,
	tunedResults *tune.TunedResults,
) error {
	// convert *mat.Dense to slice of string slices to store covariance matrix in VA status
	covMatrixStatus := denseMatrixToSliceOfStrings(tunedResults.Covariance)

	va.Status.TunerPerfData = llmdVariantAutoscalingV1alpha1.TunerPerfData{
		Model:       model,
		Accelerator: accelerator,
		UpdatedAt:   metav1.NewTime(time.Now()),
		PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
			DecodeParms: map[string]string{
				"alpha": fmt.Sprintf("%f", tunedResults.ServiceParms.Decode.Alpha),
				"beta":  fmt.Sprintf("%f", tunedResults.ServiceParms.Decode.Beta),
			},
			PrefillParms: map[string]string{
				"gamma": fmt.Sprintf("%f", tunedResults.ServiceParms.Prefill.Gamma),
				"delta": fmt.Sprintf("%f", tunedResults.ServiceParms.Prefill.Delta),
			},
		},
		NIS:              fmt.Sprintf("%f", tunedResults.NIS),
		CovarianceMatrix: covMatrixStatus,
	}
	return nil
}

func denseMatrixToSliceOfStrings(m *mat.Dense) [][]string {
	r, c := m.Dims()
	result := make([][]string, r)

	for i := 0; i < r; i++ {
		result[i] = make([]string, c)
		for j := 0; j < c; j++ {
			result[i][j] = fmt.Sprintf("%v", m.At(i, j))
		}
	}
	return result
}

func findServerInSystemData(systemData *infernoConfig.SystemData, serverName string) *infernoConfig.ServerSpec {
	for i := range systemData.Spec.Servers.Spec {
		if systemData.Spec.Servers.Spec[i].Name == serverName {
			return &systemData.Spec.Servers.Spec[i]
		}
	}
	return nil
}
