package main

// association.go: CorpusPRF - corpus co-occurrence association matrix + Lang-free
// pseudo-relevance feedback (Rocchio-style). Built incrementally at ingest; query
// expansion costs zero network and is deterministic.

import (
	"sort"
)

type AssocIdx struct {
	Pairs map[string]map[string]float64 // w1 -> {w2: assoc weight}
	Freq  map[string]float64            // document frequency proxy for rare-term gating
	DF    map[string]int                // exact DF reuse
	N     int                           // units seen
}

func NewAssocIdx() *AssocIdx {
	return &AssocIdx{
		Pairs: map[string]map[string]float64{},
		Freq:  map[string]float64{},
	}
}

func (a *AssocIdx) Update(toks []string, df map[string]int) {
	seen := map[string]bool{}
	for _, w := range toks {
		if !seen[w] {
			a.Freq[w]++
			seen[w] = true
		}
	}
	for i := 0; i < len(toks); i++ {
		for j := i + 1; j < len(toks); j++ {
			if toks[i] == toks[j] {
				continue
			}
			w1, w2 := toks[i], toks[j]
			if a.Pairs[w1] == nil {
				a.Pairs[w1] = map[string]float64{}
			}
			if a.Pairs[w2] == nil {
				a.Pairs[w2] = map[string]float64{}
			}
			a.Pairs[w1][w2] += 1.0 / (1.0 + a.Freq[w2])
			a.Pairs[w2][w1] += 1.0 / (1.0 + a.Freq[w1])
		}
	}
}

// Expand returns Rocchio-weighted expansion terms: q' = a*q + b*sum_assoc
// gterm only considers near-unig frequent terms whose association mass is
// dominated by few corpus contexts (rareness gate: Freq < docCount/8).
func (a *AssocIdx) Expand(qt []string, budget int) []string {
	if len(a.Pairs) == 0 || budget <= 0 {
		return nil
	}
	score := map[string]float64{}
	for _, w := range qt {
		if a.Pairs[w] == nil {
			continue
		}
		for w2, m := range a.Pairs[w] {
			if a.Freq[w2]*8 > float64(maxInt(a.N, 1)) {
				continue // too common, skip (rareness gate)
			}
			score[w2] += m
		}
	}
	type se struct {
		w string
		s float64
	}
	var all []se
	for w, s := range score {
		all = append(all, se{w, s})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	var out []string
	for _, e := range all {
		if len(out) >= budget {
			break
		}
		in := false
		for _, q0 := range qt {
			if q0 == e.w {
				in = true
			}
		}
		if in {
			continue
		}
		out = append(out, e.w)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
