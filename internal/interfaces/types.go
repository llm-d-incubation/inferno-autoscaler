package controller

// Captures response from ModelAnalyzer(s) per model
type ModelAnalyzeResponse struct {
	RequiredPrefillQPS float64
	RequiredDecodeQPS  float64
	Reason             string
}

// Represents additional metrics snapshot which
// ModelAnalyzer or Optimizer could consume
type MetricsSnapshot struct {
	ActualQPS float64
}
