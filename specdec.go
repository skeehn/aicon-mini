package main

import (
	"math"
)

var DraftRegistry = map[string]string{
	"meta-llama/Llama-3.1-8B-Instruct":       "yuhuili/EAGLE3-LLaMA3.1-8B",
	"meta-llama/Llama-3.1-70B-Instruct":      "yuhuili/EAGLE3-LLaMA3.1-70B",
	"meta-llama/Llama-3.3-70B-Instruct":      "yuhuili/EAGLE3-LLaMA3.3-Instruct-70B",
	"Qwen/Qwen2.5-7B-Instruct":               "yuhuili/EAGLE3-Qwen2.5-7B",
	"Qwen/Qwen2.5-72B-Instruct":              "yuhuili/EAGLE3-Qwen2.5-72B",
	"mistralai/Mistral-7B-Instruct-v0.3":     "yuhuili/EAGLE3-Mistral-7B",
	"microsoft/Phi-3.5-mini-instruct":        "yuhuili/EAGLE3-Phi-3.5-mini",
	"google/gemma-2-9b-it":                   "yuhuili/EAGLE3-Gemma-2-9B",
	"google/gemma-2-27b-it":                  "yuhuili/EAGLE3-Gemma-2-27B",
}

type SpecConfig struct {
	Model                    string
	NumSpeculativeTokens     int
	Method                   string
	DraftTensorParallelSize  int
}

func DefaultSpecConfig(model string) SpecConfig {
	tokens := 3
	if model == "yuhuili/EAGLE3-LLaMA3.1-70B" || model == "yuhuili/EAGLE3-Qwen2.5-72B" {
		tokens = 5
	}
	return SpecConfig{
		Model:                   DraftRegistry[model],
		NumSpeculativeTokens:    tokens,
		Method:                  "eagle3",
		DraftTensorParallelSize: 1,
	}
}

type SpecMetrics struct {
	AcceptedTokens int
	TotalTokens    int
	AcceptanceRate float64
	Speedup        float64
	Disabled       bool
}

func (m *SpecMetrics) Record(accepted, total int) {
	m.AcceptedTokens += accepted
	m.TotalTokens += total
	if m.TotalTokens > 0 {
		m.AcceptanceRate = float64(m.AcceptedTokens) / float64(m.TotalTokens)
	}
	m.Speedup = 1.0 + m.AcceptanceRate*0.8
	if m.TotalTokens >= 100 && m.AcceptanceRate < 0.65 {
		m.Disabled = true
	}
}

func (m *SpecMetrics) ShouldDisable() bool {
	return m.Disabled
}

func EstimateSpecSpeedup(acceptanceRate float64, numTokens int) float64 {
	return 1.0 + acceptanceRate*math.Log(float64(numTokens+1))*0.5
}

func SpecDecodeEnabled(model string, metrics *SpecMetrics) bool {
	if _, ok := DraftRegistry[model]; !ok {
		return false
	}
	if metrics != nil && metrics.Disabled {
		return false
	}
	return true
}
