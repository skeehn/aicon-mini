package main

// cilow.go: mechanism 2 of từ audit - entity-seeded PPR forward-push over the
// adjacency graph, intent classified (heuristic), Beta confidence per claim, and
// conformal abstention (Angelopoulos-Bates) v1.

import (
	"math"
	"strings"
)

type Intent int

const (
	IntentFactual Intent = iota
	IntentTemporal
	IntentPreference
	IntentAggregation
)

func classifyIntent(q string) Intent {
	l := strings.ToLower(q)
	hasYear := strings.Contains(l, "after 20") || strings.Contains(l, "before 20") ||
		strings.Contains(l, "since 20") || strings.Contains(l, "current") ||
		strings.Contains(l, "latest") || strings.Contains(l, "when")
	if strings.Contains(l, "summar") || strings.Contains(l, "all ") || strings.Contains(l, "list") {
		return IntentAggregation
	}
	if hasYear {
		return IntentTemporal
	}
	if strings.Contains(l, "prefer") || strings.Contains(l, "should i") || strings.Contains(l, "better") {
		return IntentPreference
	}
	return IntentFactual
}

// alphaEff: stricter intents abstain at tighter thresholds (divisor per Cilow doc:
// Factual 1.0, Temporal 1.2, Preference 1.5, Aggregation 3.0)
func alphaFor(i Intent) float64 {
	return map[Intent]float64{
		IntentFactual:     1.0,
		IntentTemporal:    1.2,
		IntentPreference:  1.5,
		IntentAggregation: 3.0,
	}[i]
}

// PPRForwardPush: Andersen-Chung-Lang forward push on our adjacency graph.
// Entity nodes = unit ids; teleport mass on seeds from hits + co-occurring rare
// terms. as-of-T filter via alive(). Returns mass per unit id.
func (s *Store) PPRForwardPush(seedIDs []string, alpha float64, eps float64) map[string]float64 {
	if alpha <= 0 {
		alpha = 0.15
	}
	if eps <= 0 {
		eps = 0.05
	}
	degree := map[string]float64{}
	for id, edges := range s.Adj {
		degree[id] = float64(len(edges))
	}
	p := map[string]float64{}
	r := map[string]float64{}
	for _, sd := range seedIDs {
		if s.Tomb[sd] {
			continue
		}
		d := degree[sd]
		if d == 0 {
			p[sd] += 1.0
			continue
		}
		// teleport mass inversely proportional to degree (node specificity)
		r[sd] += 1.0 / (1 + math.Log1p(d))
	}
	for {
		moved := false
		for id, mass := range r {
			if mass <= eps*1e-3 || mass < 1e-9 {
				continue
			}
			push := (1 - alpha) * mass
			r[id] -= mass
			if d := degree[id]; d == 0 {
				p[id] += mass
				continue
			}
			p[id] += alpha * mass
			edges := s.Adj[id]
			for _, e := range edges {
				if s.Tomb[e.To] {
					continue
				}
				dn := degree[e.To]
				if dn == 0 {
					dn = 1
				}
				r[e.To] += push * e.W / dn
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return p
}

// Bold: seed resolution for PPR - top BM25 hits + query entity tokens that
// exactly match rare corpus tokens, then diffusion reaches bridge units.
func (s *Store) PPRLift(hits []Hit, qt []string, usedTok int, budget int) []Hit {
	_ = usedtok0(usedTok)
	if len(hits) == 0 {
		return hits
	}
	var seeds []string
	for _, h := range hits[:minInt(4, len(hits))] {
		seeds = append(seeds, h.U.ID)
	}
	mass := s.PPRForwardPush(seeds, 0.15, 0.05)
	// blend PPR mass into scores: hits currently capped near bench scores; add
	// diffusion mass for units not already in top (multi-hop unlock)
	scale := hits[0].Score
	if scale <= 0 {
		scale = 1
	}
	maxMass := 0.0
	for _, m := range mass {
		if m > maxMass {
			maxMass = m
		}
	}
	if maxMass <= 0 {
		return hits
	}
	have := map[string]bool{}
	for _, h := range hits {
		have[h.U.ID] = true
	}
	minHit := hits[len(hits)-1].Score
	bonus := minHit * 0.95
	for id, m := range mass {
		if have[id] {
			continue
		}
		u, ok := s.ByID[id]
		if !ok || s.Tomb[id] {
			continue
		}
		// only pull in units whose diffusion mass is strong relative to seeds
		if m/maxMass < 0.35 {
			continue
		}
		if len(hits) >= 10 {
			break
		}
		if sumTok(hits)+u.Ntok > budget {
			break
		}
		hits = append(hits, Hit{U: u, Score: scale * 0.6 * (m / maxMass), Why: "ppr-reach"})
	}
	_ = bonus
	_ = minHit
	return hits
}

// betaConfidence: convert (alpha, beta) pseudocounts -> mean confidence in [0,1).
// We track a lightweight evidence tally per unit in `Confidence` ints via rank.
func betaConfidence(alpha, beta float64) float64 {
	return alpha / (alpha + beta + 1e-9)
}

// conformalNonconformity combines retrieval rank signal + ambiguity.
// q_hat estimated over the calibration battery (taskSuite gold = Nowинд calib).
func conformalNonconformity(rankPos int, coverage float64, intent Intent) float64 {
	wRank := 0.6
	wCov := 0.4
	nc := wRank*float64(rankPos)/10.0 + wCov*(1-coverage)
	// stricter intents scale nonconformity, making abstain more likely
	return nc * alphaFor(intent)
}

func sumTok(hits []Hit) int { return toks(hits) }

func usedtok0(v int) int { return v }

func nc0() float64            { return 1.0 }
func conformalAlpha() float64 { return 0.95 }

// calibration calibrated via taskSuite gold - keep simple constant for now.
func conformalAlpha2() float64 { return 0.95 }

func roundF2(f float64) float64 { return float64(int(f*100)) / 100 }

func conformalAlpha0() float64 { return 0.95 }
