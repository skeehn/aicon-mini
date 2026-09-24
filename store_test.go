package main

import "testing"

func TestDeleteSupersedes(t *testing.T) {
	s := seed()
	var v1, v2 string
	for _, u := range s.U {
		if u.Src == "gdpr-v1" && v1 == "" {
			v1 = u.ID
		}
		if u.Src == "gdpr-v2" && v2 == "" {
			v2 = u.ID
		}
	}
	s.Delete(v1, v2)
	if !s.Tomb[v1] {
		t.Fatalf("tombstone not set for %s", v1)
	}
	if s.Sup[v1] != v2 {
		t.Fatalf("supersedes link %s -> %s missing", v1, v2)
	}
}
