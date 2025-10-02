package analyzer_test

import (
	"testing"

	"github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/analyzer"
)

var testConfig = &analyzer.Configuration{
	MaxBatchSize: 8,
	MaxQueueSize: 16,
	ServiceParms: &analyzer.ServiceParms{
		Prefill: &analyzer.PrefillParms{
			Gamma: 10.0,
			Delta: 0.001,
		},
		Decode: &analyzer.DecodeParms{
			Alpha: 1.0,
			Beta:  0.01,
		},
	},
}

func TestNewQueueAnalyzer(t *testing.T) {
	tests := []struct {
		name        string // description of this test case
		qConfig     *analyzer.Configuration
		requestSize *analyzer.RequestSize
		wantErr     bool
	}{
		{
			name:        "no prefill",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: 0, AvgOutputTokens: 10},
			wantErr:     false,
		},
		{
			name:        "no prefill, one output token",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: 0, AvgOutputTokens: 1},
			wantErr:     false,
		},
		{
			name:        "no decode",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: 100, AvgOutputTokens: 1},
			wantErr:     false,
		},
		{
			name:        "mixed prefill and decode",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: 200, AvgOutputTokens: 20},
			wantErr:     false,
		},
		{
			name:        "zero input and output tokens",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: 0, AvgOutputTokens: 0},
			wantErr:     true,
		},
		{
			name:        "negative tokens",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: -1, AvgOutputTokens: -1},
			wantErr:     true,
		},
		{
			name:        "no decode, no first output token",
			qConfig:     testConfig,
			requestSize: &analyzer.RequestSize{AvgInputTokens: 50, AvgOutputTokens: 0},
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotErr := analyzer.NewQueueAnalyzer(tt.qConfig, tt.requestSize)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("NewQueueAnalyzer() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("NewQueueAnalyzer() succeeded unexpectedly")
			}
		})
	}
}
