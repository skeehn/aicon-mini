package main

import "testing"

func TestBeamDepth(t *testing.T) {
	s := seed()
	q := "Build error E1847"
	hits := s.Search(q, 120, 5)
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	for _, h := range hits {
		if h.Why == "" {
			t.Errorf("empty Why field for hit %s", h.U.Src)
		}
	}
}