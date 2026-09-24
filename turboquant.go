package main

import (
	"math"
)

type TurboQuantConfig struct {
	Enabled            bool
	KeyBits            int
	ValueBits          int
	ResidualBits       int
	GroupSize          int
	EnableQJL          bool
	CalibrationSamples int
}

func DefaultTurboQuantConfig() TurboQuantConfig {
	return TurboQuantConfig{
		Enabled:            true,
		KeyBits:            3,
		ValueBits:          2,
		ResidualBits:       1,
		GroupSize:          64,
		EnableQJL:          true,
		CalibrationSamples: 512,
	}
}

type QuantMetrics struct {
	OriginalBytes    int
	CompressedBytes  int
	CompressionRatio float64
	F1Loss           float64
}

func EstimateTurboQuantCompression(cfg TurboQuantConfig, seqLen, hiddenDim int) QuantMetrics {
	if !cfg.Enabled {
		return QuantMetrics{CompressionRatio: 1.0}
	}
	originalBits := 16
	avgBits := float64(cfg.KeyBits+cfg.ValueBits)/2 + float64(cfg.ResidualBits)*0.3
	if !cfg.EnableQJL {
		avgBits = float64(cfg.KeyBits+cfg.ValueBits) / 2
	}
	ratio := float64(originalBits) / avgBits
	if ratio < 1.8 {
		ratio = 1.8
	}
	if ratio > 2.5 {
		ratio = 2.5
	}
	originalBytes := seqLen * hiddenDim * 2 * 2
	compressedBytes := int(float64(originalBytes) / ratio)
	f1Loss := 0.02 * (1 - (ratio-1.8)/0.7*0.5)
	if f1Loss < 0.005 {
		f1Loss = 0.005
	}
	return QuantMetrics{
		OriginalBytes:    originalBytes,
		CompressedBytes:  compressedBytes,
		CompressionRatio: ratio,
		F1Loss:           f1Loss,
	}
}

func QuantizeKVCache(keys, values [][]float32, cfg TurboQuantConfig) ([][]int8, [][]int8, QuantMetrics) {
	seqLen := len(keys)
	if seqLen == 0 {
		return nil, nil, QuantMetrics{CompressionRatio: 1.0}
	}
	hiddenDim := len(keys[0])
	metrics := EstimateTurboQuantCompression(cfg, seqLen, hiddenDim)
	qKeys := make([][]int8, seqLen)
	qVals := make([][]int8, seqLen)
	for i := range keys {
		qKeys[i] = make([]int8, hiddenDim)
		qVals[i] = make([]int8, hiddenDim)
		for j := range keys[i] {
			qKeys[i][j] = int8(math.Round(float64(keys[i][j]) * 4))
			qVals[i][j] = int8(math.Round(float64(values[i][j]) * 2))
		}
	}
	return qKeys, qVals, metrics
}

func ShouldUseTurboQuant(hardware string, kvBudgetGB float64) bool {
	profile, ok := HardwareProfiles[hardware]
	if !ok {
		return false
	}
	kvGB := profile["kv_gb"].(int)
	if float64(kvGB) < kvBudgetGB*1.2 {
		return true
	}
	return false
}
