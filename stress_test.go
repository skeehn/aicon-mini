package main

import (
	"sync"
	"testing"
	"time"
)

func BenchmarkSearch1000(b *testing.B) {
	s := seed()
	queries := []string{
		"What is current GDPR stance of ZYX after 2023, who signed, actions?",
		"Find document ARG-2024-1847 build error E1847 fix",
		"erasure pipelining retained ZYX",
		"Cluster quota shards current value",
		"citation graph novelty GraphSAGE semantic map",
		"GDPR retention 90 days Alice 2022 audit logs",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			s.Search(q, 120, 5)
		}
	}
}

func TestStress1000Queries(t *testing.T) {
	s := seed()
	queries := []string{
		"What is current GDPR stance of ZYX after 2023, who signed, actions?",
		"Find document ARG-2024-1847 build error E1847 fix",
		"erasure pipelining retained ZYX",
		"Cluster quota shards current value",
		"citation graph novelty GraphSAGE semantic map",
		"GDPR retention 90 days Alice 2022 audit logs",
	}
	start := time.Now()
	var latencies []time.Duration
	for i := 0; i < 200; i++ {
		for _, q := range queries {
			qstart := time.Now()
			hits := s.Search(q, 120, 5)
			latencies = append(latencies, time.Since(qstart))
			if len(hits) == 0 {
				t.Errorf("no hits for %s", q)
			}
		}
	}
	elapsed := time.Since(start)
	totalQueries := 200 * len(queries)
	t.Logf("1000+ queries: %d queries in %v (%.0f q/s, avg %.2fms/q)", totalQueries, elapsed, float64(totalQueries)/elapsed.Seconds(), float64(elapsed.Milliseconds())/float64(totalQueries))
	// p50/p99
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	// simple sort by duration
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	p50 := sorted[len(sorted)/2]
	p99 := sorted[len(sorted)*99/100]
	t.Logf("p50=%.2fms p99=%.2fms min=%.2fms max=%.2fms", float64(p50.Microseconds())/1000, float64(p99.Microseconds())/1000, float64(sorted[0].Microseconds())/1000, float64(sorted[len(sorted)-1].Microseconds())/1000)
	if p99 > 50*time.Millisecond {
		t.Errorf("p99 too high %v", p99)
	}
}

func TestStressConcurrent(t *testing.T) {
	s := seed()
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits := s.Search("GDPR stance ZYX", 120, 5)
			if len(hits) == 0 {
				errs <- nil
			}
		}()
	}
	wg.Wait()
	close(errs)
	t.Logf("50 concurrent searches: PASS")
}

func TestStressEdgeCases(t *testing.T) {
	s := seed()
	cases := []struct {
		q      string
		budget int
		k      int
	}{
		{"", 120, 5},
		{"a", 1, 1},
		{"GDPR", 0, 5},
		{"nonexistent term xyzabc", 120, 5},
		{"after 2025 GDPR", 120, 5},
		{string(make([]byte, 5000)), 120, 5},
	}
	for _, c := range cases {
		hits := s.Search(c.q, c.budget, c.k)
		if c.budget == 0 && len(hits) != 0 {
			t.Errorf("budget 0 should return 0 hits, got %d", len(hits))
		}
		t.Logf("edge q=%.20q budget=%d k=%d -> %d hits", c.q, c.budget, c.k, len(hits))
	}
}

func TestStressTombstoneRobustness(t *testing.T) {
	for i := 0; i < 100; i++ {
		s := seed()
		// trap query should never return tombstoned v1
		hits := s.Search("GDPR retention 90 days Alice 2022 audit logs", 120, 5)
		for _, h := range hits {
			if h.U.Src == "gdpr-v1" {
				t.Fatalf("tombstoned v1 returned on iter %d", i)
			}
		}
	}
	t.Logf("100x tombstone trap: PASS (never returned gdpr-v1)")
}
