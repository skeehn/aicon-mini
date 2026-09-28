package main

// coaccess.go: Cilow-prong self-improving graph.
// Policy (user-approved): on every successful query that returns results, create
// Related edges ("coaccess") between units that co-appeared in the same query's
// top-5. Weight = 1/(cooccur+1), capped at 3 new edges per unit per call.
// Deterministic, mutex-guarded, no LLM.

import (
	"sync"
)

type CoAccess struct {
	mu    sync.Mutex
	Edges map[string]map[string]float64 // unitId -> otherId -> weight
	Hist  map[string]int                // unitId -> co-access count
	Feeds int                           // total co-recall feedings
}

func NewCoAccess() *CoAccess {
	return &CoAccess{Edges: map[string]map[string]float64{}, Hist: map[string]int{}}
}

func (c *CoAccess) Feed(hits []Hit, s *Store) int {
	if len(hits) < 2 || s == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	L := len(hits)
	if L > 5 {
		L = 5
	}
	top := hits[:L]
	for i := 0; i < L && n < 3; i++ {
		for j := i + 1; j < L && n < 3; j++ {
			a, b := top[i], top[j]
			if a.U.ID == b.U.ID {
				continue
			}
			if containsEdge(s.Adj[a.U.ID], b.U.ID) {
				continue
			}
			w := 0.85 - 0.05*float64(j-i)
			s.Adj[a.U.ID] = append(s.Adj[a.U.ID], Rel{b.U.ID, "coaccess", w})
			s.Adj[b.U.ID] = append(s.Adj[b.U.ID], Rel{a.U.ID, "coaccess", w})
			if c.Edges[a.U.ID] == nil {
				c.Edges[a.U.ID] = map[string]float64{}
			}
			c.Edges[a.U.ID][b.U.ID] = w
			c.Hist[a.U.ID]++
			c.Hist[b.U.ID]++
			n++
		}
	}
	c.Feeds++
	return n
}

func containsEdge(edges []Rel, to string) bool {
	for _, e := range edges {
		if e.To == to {
			return true
		}
	}
	return false
}

func (c *CoAccess) Stats() (int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sum := 0
	for _, m := range c.Edges {
		sum += len(m)
	}
	return c.Feeds, sum, len(c.Hist)
}
