package main

import (
	"math"
	"strings"
)

var Routes = map[string][2]string{
	"support/reset":        {"phi3.5:3.8b", "cpu"},
	"support/billing":      {"phi3.5:3.8b", "cpu"},
	"support/troubleshoot": {"llama3.2:3b", "cpu"},
	"coding/generate":      {"meta-llama/Llama-3.1-70B-Instruct", "h100_80gb"},
	"coding/review":        {"meta-llama/Llama-3.1-70B-Instruct", "h100_80gb"},
	"research/summarize":   {"meta-llama/Llama-3.1-8B-Instruct", "a100_80gb"},
}

type RouteMetrics struct {
	Quality float64
	Cost    float64
	Latency float64
	Count   int
}

var routeHistory = map[string]*RouteMetrics{}

func RouteForTask(taskClass string) (model, hardware string) {
	if r, ok := Routes[taskClass]; ok {
		return r[0], r[1]
	}
	if strings.HasPrefix(taskClass, "support/") {
		return "phi3.5:3.8b", "cpu"
	}
	if strings.HasPrefix(taskClass, "coding/") {
		return "meta-llama/Llama-3.1-70B-Instruct", "h100_80gb"
	}
	if strings.HasPrefix(taskClass, "research/") {
		return "meta-llama/Llama-3.1-8B-Instruct", "a100_80gb"
	}
	return "meta-llama/Llama-3.1-8B-Instruct", "a100_80gb"
}

func RouteWithHint(prompt, taskHint string) (model, hardware string) {
	if taskHint != "" {
		return RouteForTask(taskHint)
	}
	lower := strings.ToLower(prompt)
	if strings.Contains(lower, "reset") || strings.Contains(lower, "password") {
		return RouteForTask("support/reset")
	}
	if strings.Contains(lower, "billing") || strings.Contains(lower, "payment") {
		return RouteForTask("support/billing")
	}
	if strings.Contains(lower, "debug") || strings.Contains(lower, "error") {
		return RouteForTask("support/troubleshoot")
	}
	if strings.Contains(lower, "write code") || strings.Contains(lower, "generate") {
		return RouteForTask("coding/generate")
	}
	if strings.Contains(lower, "summarize") || strings.Contains(lower, "research") {
		return RouteForTask("research/summarize")
	}
	return RouteForTask("research/summarize")
}

func LearnFromTrace(taskClass, model, hardware string, quality, cost, latency float64) {
	key := taskClass + "|" + model + "|" + hardware
	m, ok := routeHistory[key]
	if !ok {
		m = &RouteMetrics{}
		routeHistory[key] = m
	}
	alpha := 0.1
	if m.Count == 0 {
		m.Quality = quality
		m.Cost = cost
		m.Latency = latency
	} else {
		m.Quality = m.Quality*(1-alpha) + quality*alpha
		m.Cost = m.Cost*(1-alpha) + cost*alpha
		m.Latency = m.Latency*(1-alpha) + latency*alpha
	}
	m.Count++
}

func ScoreRoute(taskClass, model, hardware string) float64 {
	key := taskClass + "|" + model + "|" + hardware
	m, ok := routeHistory[key]
	if !ok {
		return 0.5
	}
	costNorm := 1 / (1 + m.Cost)
	latencyNorm := 1 / (1 + m.Latency/1000)
	return 0.5*m.Quality + 0.3*costNorm + 0.2*latencyNorm
}

func EstimateCost(model string, promptTokens, completionTokens int) float64 {
	pricing := map[string][2]float64{
		"phi3.5:3.8b":                          {0.0001, 0.0002},
		"llama3.2:3b":                          {0.0002, 0.0004},
		"meta-llama/Llama-3.1-8B-Instruct":     {0.0006, 0.0012},
		"meta-llama/Llama-3.1-70B-Instruct":    {0.0025, 0.005},
	}
	if p, ok := pricing[model]; ok {
		return float64(promptTokens)/1000*p[0] + float64(completionTokens)/1000*p[1]
	}
	return float64(promptTokens+completionTokens) / 1000 * 0.001
}

func RouteCostSavings(originalModel, routedModel string, promptTokens int) float64 {
	origCost := EstimateCost(originalModel, promptTokens, 0)
	routedCost := EstimateCost(routedModel, promptTokens, 0)
	saving := origCost - routedCost
	if saving < 0 {
		saving = 0
	}
	return math.Round(saving*10000) / 10000
}
