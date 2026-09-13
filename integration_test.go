package main

import (
	"testing"
)

func TestSpecDecRegistry(t *testing.T) {
	if len(DraftRegistry) != 9 {
		t.Errorf("expected 9 draft models, got %d", len(DraftRegistry))
	}
	cfg := DefaultSpecConfig("meta-llama/Llama-3.1-8B-Instruct")
	if cfg.Method != "eagle3" || cfg.NumSpeculativeTokens != 3 {
		t.Errorf("bad spec config: %+v", cfg)
	}
	m := &SpecMetrics{}
	m.Record(70, 100)
	if m.AcceptanceRate != 0.7 {
		t.Errorf("acceptance %f", m.AcceptanceRate)
	}
	if m.Speedup < 1.4 {
		t.Errorf("speedup too low %f", m.Speedup)
	}
	m2 := &SpecMetrics{}
	for i := 0; i < 100; i++ {
		m2.Record(60, 100)
	}
	if !m2.ShouldDisable() {
		t.Error("should disable low acceptance")
	}
	if !SpecDecodeEnabled("meta-llama/Llama-3.1-8B-Instruct", nil) {
		t.Error("should enable known model")
	}
	if SpecDecodeEnabled("unknown/model", nil) {
		t.Error("should not enable unknown")
	}
}

func TestTurboQuant(t *testing.T) {
	cfg := DefaultTurboQuantConfig()
	if cfg.KeyBits != 3 || cfg.ValueBits != 2 || cfg.ResidualBits != 1 {
		t.Errorf("bad quant config %+v", cfg)
	}
	m := EstimateTurboQuantCompression(cfg, 512, 4096)
	if m.CompressionRatio < 1.8 || m.CompressionRatio > 2.5 {
		t.Errorf("ratio out of bounds %f", m.CompressionRatio)
	}
	if m.F1Loss > 0.02 {
		t.Errorf("F1 loss too high %f", m.F1Loss)
	}
	keys := [][]float32{{0.1, 0.2}, {0.3, 0.4}}
	vals := [][]float32{{0.5, 0.6}, {0.7, 0.8}}
	_, _, qm := QuantizeKVCache(keys, vals, cfg)
	if qm.CompressionRatio < 1.0 {
		t.Error("quant should compress")
	}
}

func TestPDTopologies(t *testing.T) {
	for _, topo := range []PDTopology{SingleNode, MultiNodePrefill, MultiNodeDecode, HybridTopology} {
		m := EstimatePDMetrics(topo, 512)
		if m.GoodputSpeedup < 1.0 {
			t.Errorf("topo %s bad speedup %f", topo, m.GoodputSpeedup)
		}
		if m.TTFTMs > 2500 || m.TTFTMs < 500 {
			t.Errorf("topo %s TTFT %f", topo, m.TTFTMs)
		}
	}
	if !ShouldUsePD(8, 2048) {
		t.Error("should use PD for 8 GPUs")
	}
	if ShouldUsePD(2, 512) {
		t.Error("should not use PD for 2 GPUs short seq")
	}
	data := []byte{1, 2, 3}
	migrated := MigrateKV(data, "h100_80gb", "a100_80gb", "meta-llama/Llama-3.1-8B-Instruct", "vllm")
	if len(migrated) != 3 {
		t.Error("migration length")
	}
	if migrated[0] == data[0] {
		t.Error("cross-hardware should transform")
	}
	same := MigrateKV(data, "h100_80gb", "h100_80gb", "model", "vllm")
	if same[0] != data[0] {
		t.Error("same hardware should not transform")
	}
}

func TestRouter(t *testing.T) {
	m, hw := RouteForTask("support/reset")
	if m != "phi3.5:3.8b" || hw != "cpu" {
		t.Errorf("support/reset route %s %s", m, hw)
	}
	m, hw = RouteWithHint("Reset my password", "")
	if m != "phi3.5:3.8b" {
		t.Errorf("hint failed %s", m)
	}
	m, hw = RouteWithHint("irrelevant", "coding/generate")
	if m != "meta-llama/Llama-3.1-70B-Instruct" {
		t.Errorf("task hint failed %s", m)
	}
	LearnFromTrace("support/reset", "phi3.5:3.8b", "cpu", 0.95, 0.001, 200)
	if ScoreRoute("support/reset", "phi3.5:3.8b", "cpu") < 0.5 {
		t.Error("score too low after learn")
	}
	cost := EstimateCost("phi3.5:3.8b", 100, 50)
	if cost <= 0 {
		t.Error("cost should be positive")
	}
	saved := RouteCostSavings("gpt-4", "phi3.5:3.8b", 4500)
	if saved <= 0 {
		t.Error("should save vs gpt-4")
	}
}

func TestSpendCaps(t *testing.T) {
	tracker := NewSpendTracker()
	cap := GetCap("test-key")
	if cap.Daily != 10000 {
		t.Errorf("cap daily %d", cap.Daily)
	}
	if !tracker.CheckAndReserve("user1", 100, cap) {
		t.Error("should reserve")
	}
	if tracker.CheckAndReserve("user1", 20000, cap) {
		t.Error("should fail over cap")
	}
	for i := 0; i < 4; i++ {
		if !tracker.CheckLoop("task1") {
			t.Error("loop should not trigger yet")
		}
	}
	if tracker.CheckLoop("task1") {
		t.Error("loop should trigger on 5th in 60s")
	}
	tracker.CreateGrant("grant1", 500, 600)
	cents, ok := tracker.UseGrant("grant1")
	if !ok || cents != 500 {
		t.Error("grant failed")
	}
	if _, ok := tracker.UseGrant("grant1"); ok {
		t.Error("grant should be one-time")
	}
	pct := CapUsedPercent(tracker, "user1")
	if pct <= 0 {
		t.Error("cap used should be >0")
	}
}

func TestUnifiedEngine(t *testing.T) {
	eng := NewUnifiedEngine(EngineConfig{
		ModelID:          "auto",
		Hardware:         "h100_80gb",
		EnableSpecDec:    true,
		EnableTurboQuant: true,
		EnablePD:         true,
		PDTopology:       HybridTopology,
	})
	if eng.Type != VLLM {
		t.Errorf("h100 should be vllm, got %s", eng.Type)
	}
	if eng.HKVD < 0.05 || eng.HKVD > 0.25 {
		t.Errorf("HKVD %f", eng.HKVD)
	}
	res, err := eng.Complete("Reset my password", "support/reset", 120)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if res.Headers["X-Cache"] != "CACHEBLEND" {
		t.Error("missing cache header")
	}
	if res.Headers["X-Saved"] == "" {
		t.Error("missing saved header")
	}
	if eng.TTFT() <= 0 || eng.TPOT() <= 0 {
		t.Error("TTFT/TPOT should be positive")
	}
	cpuEng := NewUnifiedEngine(EngineConfig{ModelID: "phi3.5:3.8b", Hardware: "cpu"})
	if cpuEng.Type != MLC {
		t.Errorf("cpu should be mlc, got %s", cpuEng.Type)
	}
}
