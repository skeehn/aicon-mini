package main

import "testing"

func TestHybridPrecedence(t *testing.T) {
	s := seed()
	h := s.Search("GDPR retention 90 days", 120, 5)
	if h[0].U.Src != "gdpr-v2" && h[0].U.Src != "gdpr-v2-dup" {
		t.Errorf("expected non-tombstoned gdpr-v2 first, got %s", h[0].U.Src)
	}
}