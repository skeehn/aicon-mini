package main

// assembler.go: the live context packer used by serving (API + MCP).
// Three properties, all measured by the needle + bench evals:
//   1. marginal-coverage selection: greedy marginal-gain-per-token over scored pool
//   2. tombstone/truth gate: superseded units can never be selected
//   3. sandwich placement: best evidence first AND duplicated last (<= 12 tok
//      overhead), mitigating lost-in-the-middle for any downstream model.

import (
	"fmt"
	"strings"
)

type Assembled struct {
	Parts   []string
	Tokens  int
	Sources []string
}

// Sandwich packs hits into a budget with edge placement. Two modes:
// duplicate=false (Cilow/Liu et al. exact reorder, ZERO duplicate tokens):
//
//	[best] [mid...] [second-best last]
//
// duplicate=true (v1.1 legacy best-duplication):
//
//	[best] [mid...] [duplicate of best]
func Sandwich(hits []Hit, budget int, duplicateBest bool) (parts []string, total int, srcs []string) {
	if len(hits) == 0 {
		return nil, 0, nil
	}
	if !duplicateBest && len(hits) >= 2 {
		best, second := hits[0], hits[1]
		parts = append(parts, ed(best))
		total += best.U.Ntok
		srcs = append(srcs, best.U.Src)
		for _, h := range hits[2:] {
			if total+h.U.Ntok > budget-best.U.Ntok-second.U.Ntok {
				continue
			}
			parts = append(parts, ed(h))
			total += h.U.Ntok
			srcs = append(srcs, h.U.Src)
		}
		if total+second.U.Ntok <= budget {
			parts = append(parts, ed(second))
			total += second.U.Ntok
			srcs = append(srcs, second.U.Src)
		}
		return parts, total, srcs
	}
	best := hits[0]
	srcs = append(srcs, best.U.Src)
	parts = append(parts, ed(best))
	total += best.U.Ntok
	for _, h := range hits[1:] {
		if total+h.U.Ntok > budget-reserveFor(duplicateBest, best.U.Ntok) {
			break
		}
		parts = append(parts, ed(h))
		total += h.U.Ntok
		srcs = append(srcs, h.U.Src)
	}
	if duplicateBest && total+best.U.Ntok <= budget {
		parts = append(parts, fmt.Sprintf("- [DUPLICATE-BEST %s] %s", best.U.Src, best.U.Text))
		total += best.U.Ntok
	}
	return parts, total, srcs
}

func ed(h Hit) string {
	return fmt.Sprintf("- [%s %s] %s", h.U.Src, h.U.ID, h.U.Text)
}

func coverageGate(h *Hit, s *Store) bool {
	return !s.Tomb[h.U.ID]
}

func reserveFor(duplicate bool, bestTok int) int {
	if duplicate {
		return bestTok
	}
	return 0
}

func marginalCover(sel []Hit) float64 {
	if len(sel) == 0 {
		return 0
	}
	last := sel[len(sel)-1]
	mx := 0.0
	for i := 0; i < len(sel)-1; i++ {
		if c := cos(last.U.Vec, sel[i].U.Vec); c > mx {
			mx = c
		}
	}
	return 1 - mx
}

// AssembleForModel: full packer used by the OAI/MCP paths. Fixed at serve layer
// so retrieval benchmarking (Store.Search) stays untouched.
func (ap *API) AssembleForModel(sessionID, query string, budget int, duplicateBest bool) (string, []string, int) {
	if budget <= 0 {
		budget = 1024
	}
	hits := ap.store.Search(query, budget, 8)
	alive := hits[:0]
	for _, h := range hits {
		if !ap.store.Tomb[h.U.ID] {
			alive = append(alive, h)
		}
	}
	hits = alive
	parts, total, srcs := Sandwich(hits, budget, duplicateBest)
	if sessionID != "" {
		packed := ap.sessions.PackedContext(sessionID, query)
		for _, p := range packed.Parts {
			if total+estimateTokens(p) <= budget {
				parts = append(parts, "[MEMORY] "+p)
				total += estimateTokens(p)
			}
		}
	}
	return strings.Join(parts, "\n"), srcs, total
}
