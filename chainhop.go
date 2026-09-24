package main

// chainhop.go: LLM-free iterative multi-hop retrieval.
// Round 0 rank -> extract rare evidence terms -> expand query -> re-rank with
// marginal information gain gate -> verify via graph chain adjacency.

import (
	"math"
	"sort"
)

type ChainHopResult struct {
	Hit      []Hit
	ExpTerms []string
	Hops     int
	Verifier map[string]bool
}

func (s *Store) ChainHop(query string, budget, k int) []Hit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := s.ChainHopFull(query, budget, k)
	return res.Hit
}

func (s *Store) ChainHopFull(query string, budget, k int) ChainHopResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := ChainHopResult{Verifier: map[string]bool{}}
	res0 := s.SearchBasic(query, budget, k)
	out.Hit = res0
	if len(res0) == 0 {
		return out
	}
	assoc := s.AssocPool()
	if assoc == nil {
		return out
	}
	qt := tok(query)
	var exps []string
	for _, h := range res0[:minInt(3, len(res0))] {
		exps = append(exps, assoc.Expand(append(append([]string{}, qt...), tok(h.U.Text)...), 8)...)
	}
	seen := map[string]bool{}
	var expTerms []string
	for _, e := range exps {
		if seen[e] {
			continue
		}
		seen[e] = true
		expTerms = append(expTerms, e)
	}
	out.ExpTerms = expTerms
	if len(expTerms) == 0 {
		return out
	}
	q1 := query
	for _, e := range expTerms {
		q1 += " " + e
	}
	expanded := s.rankCombined(q1, budget, k*3)
	// marginal information gain gate: only keep new units that materially add
	// rare-term coverage over round-0
	covered := map[string]bool{}
	for _, h := range res0 {
		for _, w := range h.U.Tok {
			if s.DF[w] <= s.N/2 {
				covered[w] = true
			}
		}
	}
	var round2 []Hit
	for _, h := range expanded {
		nu := 0
		for _, w := range h.U.Tok {
			if s.DF[w] <= s.N/8 && !covered[w] {
				nu++
			}
		}
		if nu >= 2 {
			round2 = append(round2, h)
		}
	}
	sort.Slice(round2, func(i, j int) bool { return round2[i].Score > round2[j].Score })
	if len(round2) > k {
		round2 = round2[:k]
	}
	out.Hops = 1
	// chain verify: each new unit must connect by graph edge (any edge, 1-hop)
	// to at least one round-0 unit id, or be direct co-occurring with unit text
	newHits := []Hit{}
	for _, h := range round2 {
		ok := false
		for _, e := range s.Adj[h.U.ID] {
			for _, r0 := range res0 {
				if e.To == r0.U.ID {
					ok = true
					out.Verifier[h.U.ID] = true
					break
				}
			}
		}
		if ok {
			newHits = append(newHits, h)
		}
	}
	minR0 := math.Inf(1)
	for _, h := range res0 {
		if h.Score < minR0 {
			minR0 = h.Score
		}
	}
	have := map[string]bool{}
	final := []Hit{}
	for _, h := range res0 {
		final = append(final, h)
		have[h.U.ID] = true
	}
	used := toks(final)
	for _, h := range newHits {
		if have[h.U.ID] || len(final) >= k || used+h.U.Ntok > budget {
			continue
		}
		h.Score = minR0 * 0.95
		h.Why = "chainhop"
		final = append(final, h)
		used += h.U.Ntok
	}
	out.Hit = final
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
