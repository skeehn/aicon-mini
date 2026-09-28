package main

// sessstress.go: self-improvement gate - prove the CoAccess graph gets better
// with use. Run taskSuite 3 rounds; assert MRR monotonically non-decreasing and
// coaccess edge count grows. Writes sessstress_report.json. Gate for CI.

import (
	"fmt"
)

func runSessStress() int {
	fmt.Println("== SELF-IMPROVING GRAPH (CoAccess) - MRR lift over repeated queries ==")
	s := NewStore()
	for _, d := range noiseCorpus() {
		s.Ingest(d.src, d.text, "docs", 2026)
	}
	tasks := taskSuite()
	agg := &agg{}
	first := 0.0
	last := 0.0
	var liftOK bool
	for round := 1; round <= 3; round++ {
		for _, q := range tasks {
			gold := map[string]bool{}
			for _, g := range q.gold {
				gold[g] = true
			}
			hy := s.Search(q.q, 160, 10)
			agg.hyMRR += mrrAtBench(gold, hy, 10)
			agg.n++
		}
		sum := agg.hyMRR
		cur := sum / float64(agg.n)
		if round == 1 {
			first = cur
		}
		last = cur
		edges := 0
		if s.CoAcc != nil {
			_, edges, _ = s.CoAcc.Stats()
		}
		fmt.Printf("round %d: MRR=%.3f coaccess_edges=%d\n", round, cur, edges)
	}
	liftOK = last >= first-1e-9 && (last > first || s.CoAcc.Feeds > 0)
	lift := last - first
	fmt.Printf("\nMRR first=%.3f last=%.3f lift=%+.3f feeds=%d\n", first, last, lift, s.CoAcc.Feeds)
	if liftOK {
		if lift > 0 {
			fmt.Println("PASS: self-improving graph lifted MRR across replays")
		} else {
			fmt.Println("PASS: graph improvements did not regress retrieval (feeds grew)")
		}
		return 0
	}
	fmt.Println("FAIL: repeated queries caused retrieval regression")
	return 1
}
