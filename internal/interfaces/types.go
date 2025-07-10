package controller

type ModelAnalyzeResponse struct {
	RequiredPrefillQPS float64
	RequiredDecodeQPS  float64
	Reason             string
}

type MetricsSnapshot struct {
	ActualQPS float64
}
