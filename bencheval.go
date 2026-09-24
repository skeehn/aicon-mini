package main

// bencheval.go: external benchmark slices (vendored) vs honest baselines.
// Baselines: bm25only, denseOnly, naiveFilesys (recency+overlap), communityRAG (GraphRAG-style walk).
// Ours: context compiler pipeline (Store.Search).
// Deterministic, hashed-embedding mode (CI-safe).

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type benchQ struct {
	ID         string   `json:"id"`
	Question   string   `json:"question"`
	GoldTitles []string `json:"gold_titles"`
	Type       string   `json:"type"`
	Level      string   `json:"level"`
	Paras      []struct {
		Title     string   `json:"title"`
		Sentences []string `json:"sentences"`
	} `json:"paras"`
}

type agg struct {
	n                               int
	hyR5, hyR10, hyMRR, hyTok       float64
	bR5, bR10, bMRR, bTok           float64
	dR5, dR10, nR5, nR10, cR5, cR10 float64
	containOk                       int
}

func loadBench(file string) []benchQ {
	b, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench skip %s: %v\n", file, err)
		return nil
	}
	var qs []benchQ
	if err := json.Unmarshal(b, &qs); err != nil {
		fmt.Fprintf(os.Stderr, "bench bad %s: %v\n", file, err)
		return nil
	}
	return qs
}

func buildStore(q benchQ) *Store {
	s := NewStore()
	for _, p := range q.Paras {
		s.Ingest("bench-"+p.Title, strings.Join(p.Sentences, " "), "docs", 2026)
	}
	return s
}

func goldMap(q benchQ) map[string]bool {
	g := map[string]bool{}
	for _, t := range q.GoldTitles {
		g["bench-"+t] = true
	}
	return g
}

func naiveFileBaseline(s *Store, query string, budget, k int) []Hit {
	qt := tok(query)
	type nw struct {
		u  *Unit
		ov int
	}
	var all []nw
	for _, u := range s.U {
		if s.Tomb[u.ID] {
			continue
		}
		set := map[string]bool{}
		for _, w := range u.Tok {
			set[w] = true
		}
		ov := 0
		for _, w := range qt {
			if set[w] {
				ov++
			}
		}
		all = append(all, nw{u, ov})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].u.Time != all[j].u.Time {
			return all[i].u.Time > all[j].u.Time
		}
		return all[i].ov > all[j].ov
	})
	var out []Hit
	used := 0
	for _, x := range all {
		if len(out) >= k || used+x.u.Ntok > budget {
			break
		}
		out = append(out, Hit{U: x.u, Score: float64(x.ov), Why: "naive"})
		used += x.u.Ntok
	}
	return out
}

func communityBaseline(s *Store, query string, budget, k int) []Hit {
	qt := tok(query)
	best := ""
	bestScore := -1.0
	for _, u := range s.U {
		if s.Tomb[u.ID] {
			continue
		}
		sc := s.bm25(qt, u)
		if sc > bestScore {
			bestScore = sc
			best = u.ID
		}
	}
	if best == "" {
		return nil
	}
	var out []Hit
	seen := map[string]bool{best: true}
	if u, ok := s.ByID[best]; ok {
		out = append(out, Hit{U: u, Score: bestScore, Why: "community"})
	}
	cur := []string{best}
	for depth := 0; depth < 2 && len(out) < k; depth++ {
		var nxt []string
		for _, id := range cur {
			for _, e := range s.Adj[id] {
				if seen[e.To] || s.Tomb[e.To] {
					continue
				}
				seen[e.To] = true
				nxt = append(nxt, e.To)
				if u0, ok := s.ByID[e.To]; ok {
					out = append(out, Hit{U: u0, Score: s.bm25(qt, u0), Why: "community"})
				}
			}
		}
		cur = nxt
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > k {
		out = out[:k]
	}
	return out
}

func recallAny(gold map[string]bool, hits []Hit, at int) float64 {
	for i := 0; i < len(hits) && i < at; i++ {
		if gold[hits[i].U.Src] {
			return 1
		}
	}
	return 0
}

func mrrAtBench(gold map[string]bool, hits []Hit, at int) float64 {
	for i := 0; i < len(hits) && i < at; i++ {
		if gold[hits[i].U.Src] {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func runFileBench(file string, agg *agg) {
	qs := loadBench(file)
	if len(qs) == 0 {
		return
	}
	budget, k := 220, 10
	for _, q := range qs {
		s := buildStore(q)
		gold := goldMap(q)
		hy := s.Search(q.Question, budget, k)
		bm := rankOnly(s, q.Question, "bm25", budget, k)
		dn := rankOnly(s, q.Question, "dense", budget, k)
		nv := naiveFileBaseline(s, q.Question, budget, k)
		co := communityBaseline(s, q.Question, budget, k)
		agg.hyR5 += recallAny(gold, hy, 5)
		agg.hyR10 += recallAny(gold, hy, 10)
		agg.hyMRR += mrrAtBench(gold, hy, 10)
		agg.hyTok += float64(toks(hy))
		agg.bR5 += recallAny(gold, bm, 5)
		agg.bR10 += recallAny(gold, bm, 10)
		agg.bMRR += mrrAtBench(gold, bm, 10)
		agg.bTok += float64(toks(bm))
		agg.dR5 += recallAny(gold, dn, 5)
		agg.dR10 += recallAny(gold, dn, 10)
		agg.nR5 += recallAny(gold, nv, 5)
		agg.nR10 += recallAny(gold, nv, 10)
		agg.cR5 += recallAny(gold, co, 5)
		agg.cR10 += recallAny(gold, co, 10)
		hyTop := hy
		if len(hy) > 5 {
			hyTop = hy[:5]
		}
		for _, x := range hyTop {
			if gold[x.U.Src] {
				agg.containOk++
				break
			}
		}
		if recallAny(gold, hy, 10) == 0 {
			var srcs []string
			for _, x := range hy {
				srcs = append(srcs, strings.TrimPrefix(x.U.Src, "bench-"))
			}
			fmt.Printf("MISS %s | %s | top=%v | gold=%v\n", q.ID, truncS(q.Question, 70), srcs, q.GoldTitles)
		}
		agg.n++
	}
}

func runBenchEvalCmd() int {
	agg := &agg{}
	fmt.Println("== EXTERNAL BENCHMARKS (vendored) ==")
	runFileBench("data/hotpot_100.json", agg)
	runFileBench("data/musique_25.json", agg)
	if agg.n == 0 {
		fmt.Println("FAIL: no bench data found")
		return 1
	}
	n := float64(agg.n)
	fmt.Printf("n=%d\n", agg.n)
	fmt.Printf("ours   R@5=%.3f R@10=%.3f MRR=%.3f tok=%.1f\n", agg.hyR5/n, agg.hyR10/n, agg.hyMRR/n, agg.hyTok/n)
	fmt.Printf("bm25   R@5=%.3f R@10=%.3f MRR=%.3f tok=%.1f\n", agg.bR5/n, agg.bR10/n, agg.bMRR/n, agg.bTok/n)
	fmt.Printf("dense  R@5=%.3f R@10=%.3f\n", agg.dR5/n, agg.dR10/n)
	fmt.Printf("naive  R@5=%.3f R@10=%.3f\n", agg.nR5/n, agg.nR10/n)
	fmt.Printf("commun R@5=%.3f R@10=%.3f\n", agg.cR5/n, agg.cR10/n)
	fmt.Printf("containment(top-5): %d/%d = %.3f\n", agg.containOk, agg.n, float64(agg.containOk)/n)
	report := map[string]float64{
		"n":       n,
		"ours_r5": round3(agg.hyR5 / n), "ours_r10": round3(agg.hyR10 / n),
		"ours_mrr": round3(agg.hyMRR / n), "ours_tokens": round3(agg.hyTok / n),
		"bm25_r5": round3(agg.bR5 / n), "bm25_r10": round3(agg.bR10 / n),
		"bm25_mrr": round3(agg.bMRR / n), "bm25_tokens": round3(agg.bTok / n),
		"dense_r5": round3(agg.dR5 / n), "dense_r10": round3(agg.dR10 / n),
		"naive_r5": round3(agg.nR5 / n), "naive_r10": round3(agg.nR10 / n),
		"community_r5": round3(agg.cR5 / n), "community_r10": round3(agg.cR10 / n),
		"containment": round3(float64(agg.containOk) / n),
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile("bench_report.json", append(b, '\n'), 0644)
	if agg.hyR5/n >= agg.bR5/n && agg.containOk >= int(0.75*float64(agg.n)) {
		fmt.Println("PASS: ours beat bm25 R@5 and containment >= 75%")
		return 0
	}
	fmt.Println("PARTIAL: report written; see bench_report.json")
	return 1
}

func truncS(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
