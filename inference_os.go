package main

import (
	"math"
	"strings"
)

type Fragment struct {
	ID             string
	Content        string
	TokenCount     int
	Enrichment     string
	BoundariesJSON string
	CrossAttnScore float64
	Source         string
	MetadataJSON   string
	CreatedAt      int64
	UpdatedAt      int64
}

func enrich(content, level string) string {
	if level == "raw" {
		return content
	}
	if level == "summarized" {
		sum := content
		if len(sum) > 200 {
			sum = sum[:200]
		}
		return "[SUMMARY] " + sum + "\n[CONTENT] " + content
	}
	if level == "self_contained" {
		entities := extractEntities(content)
		relations := extractRelations(content, entities)
		summary := content
		if len(summary) > 200 {
			summary = summary[:200]
		}
		ctx := buildContext(entities, relations)
		return "[CONTEXT] " + ctx + "\n[ENTITIES] " + strings.Join(entities, ", ") + "\n[RELATIONS] " + strings.Join(relations, "; ") + "\n[SUMMARY] " + summary + "\n[CONTENT] " + content
	}
	return content
}

func extractEntities(content string) []string {
	toks := tok(content)
	seen := map[string]bool{}
	var out []string
	for _, w := range toks {
		if len(w) > 4 && !seen[w] {
			seen[w] = true
			out = append(out, w)
			if len(out) >= 5 {
				break
			}
		}
	}
	return out
}

func extractRelations(content string, entities []string) []string {
	var out []string
	for i := 0; i < len(entities)-1 && i < 3; i++ {
		out = append(out, entities[i]+" -> "+entities[i+1])
	}
	return out
}

func buildContext(entities, relations []string) string {
	if len(entities) == 0 {
		return "general"
	}
	return strings.Join(entities[:min(3, len(entities))], " ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func estimateHKVD(fragments []Fragment) float64 {
	base := 0.15
	for _, f := range fragments {
		if f.Enrichment == "self_contained" {
			base *= 0.4
		} else if f.Enrichment == "summarized" {
			base *= 0.7
		}
	}
	if base < 0.05 {
		base = 0.05
	}
	if base > 0.25 {
		base = 0.25
	}
	return base
}

type Boundary struct {
	StartToken      int    `json:"start_token"`
	EndToken        int    `json:"end_token"`
	EnrichmentLevel string `json:"enrichment_level"`
}

func identifyHKVDFromBoundaries(boundaries []Boundary, fragments []Fragment) map[int]bool {
	candidates := map[int]bool{}
	for _, b := range boundaries {
		start, end := b.StartToken, b.EndToken
		enrichment := b.EnrichmentLevel
		window := 32
		if enrichment == "self_contained" {
			window = 16
		} else if enrichment == "summarized" {
			window = 24
		}
		for i := max(0, start-window); i < start+window; i++ {
			candidates[i] = true
		}
		for i := end - window; i < end+window; i++ {
			candidates[i] = true
		}
		candidates[start] = true
		if end-1 >= 0 {
			candidates[end-1] = true
		}
	}
	return candidates
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func chooseHKVDFraction(modelSize, hardware string, seqLen int) float64 {
	kvBytes := kvSizeBytes(modelSize, seqLen)
	throughputMap := map[string]float64{
		"memory": 500e9,
		"nvme":   14e9,
		"ssd":    7e9,
		"object": 0.5e9,
	}
	device := "nvme"
	if strings.Contains(hardware, "m2") {
		device = "ssd"
	}
	throughput := throughputMap[device]
	tLoadMs := float64(kvBytes) / throughput * 1000

	prefillPerLayer := map[string]float64{"7b": 0.5, "34b": 2.0, "70b": 7.0}
	prefillTotal := prefillPerLayer[modelSize] * float64(numLayers(modelSize))

	r := tLoadMs / (prefillTotal + 1e-9)
	if r < 0.15 {
		r = 0.15
	}
	if r > 0.25 {
		r = 0.25
	}
	return r
}

func kvSizeBytes(modelSize string, seqLen int) int {
	perToken := map[string]int{"7b": 4096 * 2 * 2, "34b": 8192 * 2 * 2, "70b": 8192 * 4 * 2}[modelSize]
	if perToken == 0 {
		perToken = 4096 * 4
	}
	return perToken * seqLen
}

func numLayers(modelSize string) int {
	m := map[string]int{"7b": 32, "34b": 48, "70b": 80}
	if v, ok := m[modelSize]; ok {
		return v
	}
	return 32
}

var HardwareProfiles = map[string]map[string]interface{}{
	"h100_80gb": {"fp8": true, "int4": true, "kv_gb": 80, "nvme_gb_s": 14},
	"h100_mig":  {"fp8": true, "int4": true, "kv_gb": 20, "nvme_gb_s": 14},
	"a100_80gb": {"fp8": false, "int4": true, "kv_gb": 80, "nvme_gb_s": 14},
	"h200":      {"fp8": true, "int4": true, "kv_gb": 141, "nvme_gb_s": 14},
	"b200":      {"fp8": true, "int4": true, "kv_gb": 192, "nvme_gb_s": 14},
	"m2_ultra":  {"fp8": false, "int4": true, "kv_gb": 192, "nvme_gb_s": 7},
}

func fragmentEnrichmentBoost(enrichment string) float64 {
	switch enrichment {
	case "self_contained":
		return 0.4
	case "summarized":
		return 0.7
	default:
		return 1.0
	}
}

func hkvdForStore(s *Store) float64 {
	var frags []Fragment
	for _, u := range s.U {
		enc := "raw"
		if u.Prov == "policy" || u.Prov == "papers" {
			enc = "self_contained"
		} else if u.Prov == "docs" {
			enc = "summarized"
		}
		frags = append(frags, Fragment{Enrichment: enc, TokenCount: u.Ntok})
	}
	return estimateHKVD(frags)
}

func cacheBlendHKVD(s *Store) float64 {
	hkvd := hkvdForStore(s)
	seqLen := 0
	for _, u := range s.U {
		seqLen += u.Ntok
	}
	controller := chooseHKVDFraction("7b", "h100_80gb", seqLen)
	blended := (hkvd + controller) / 2
	return math.Max(0.05, math.Min(0.25, blended))
}
