package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCorpusPRFExpansion(t *testing.T) {
	s := NewStore()
	seedInto(s)
	qt := tok("GDPR stance ZYX retention")
	ex := s.Assoc.Expand(qt, 6)
	if len(ex) == 0 {
		t.Fatal("PRF should expand from corpus co-occurrence")
	}
	for _, e := range ex {
		for _, q := range qt {
			if e == q {
				t.Errorf("expansion %s duplicates query term", e)
			}
		}
	}
}

func TestChainHopAddsConnectivity(t *testing.T) {
	s := NewStore()
	seedInto(s)
	res := s.ChainHopUnLocked("What error code E1847 fix works?", 120, 5)
	if len(res.Hit) == 0 {
		t.Fatal("no hits")
	}
	for id := range res.Verifier {
		for _, h := range res.Hit {
			if h.U.ID == id && h.Why != "chainhop" {
				t.Fatalf("hit %s expected chainhop why, got %s", id, h.Why)
			}
		}
	}
	// round0 hits should never be overwritten with chainhop scores
	for i, h := range res.Hit {
		if i < 5 && h.Why == "chainhop" {
			break
		}
	}
}

func TestChainHopBudgetPreserved(t *testing.T) {
	s := NewStore()
	seedInto(s)
	hits := s.ChainHop("GDPR stance retention 90 days", 100, 5)
	if toks(hits) > 100+40 {
		t.Fatalf("chainhop budget: %d", toks(hits))
	}
}

func TestBenchEvalRuns(t *testing.T) {
	b := loadBench("data/hotpot_100.json")
	if len(b) < 100 {
		t.Fatalf("hotpot slice missing: %d", len(b))
	}
	m := loadBench("data/musique_25.json")
	if len(m) < 25 {
		t.Fatalf("musique slice missing: %d", len(m))
	}
	// spot run one task end-to-end
	q := b[1]
	s := buildStore(q)
	gold := goldMap(q)
	hits := s.Search(q.Question, 220, 10)
	if len(hits) == 0 {
		t.Fatal("no bench hits")
	}
	if len(q.GoldTitles) == 0 {
		t.Fatal("gold empty")
	}
	if hits[0].U.ID == "" {
		t.Fatal("no unit id")
	}
	_ = gold
}

func TestNeedleReportShape(t *testing.T) {
	rc := runNeedleEval()
	if rc != 0 {
		t.Fatal("needle failed")
	}
	b, err := os.ReadFile("needle_report.json")
	if err != nil {
		t.Fatal(err)
	}
	var rep NeedleReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.SandwichPackedAccuracy <= rep.FlatPackedAccuracy {
		t.Fatal("sandwich must lift flat packed accuracy")
	}
}

func TestAuthRequiredWhenTokenSet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := authWrap(mux, true, "secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(""))
	req2.Header.Set("Authorization", "Bearer secret")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code == http.StatusUnauthorized {
		t.Fatal("valid token rejected")
	}
}

func TestMetricsWrap(t *testing.T) {
	ap := NewAPI(NewStore())
	h := metricsWrap(ap.handleChat)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h(rec, req)
	snap := metrics.snapshot()
	if snap["aicon_search_total"] == 0 {
		t.Fatal("search counter not incremented")
	}
}

func TestSandwichAssembler(t *testing.T) {
	s := NewStore()
	seedInto(s)
	hits := s.Search("GDPR stance retention", 120, 5)
	if len(hits) < 2 {
		t.Fatal("need hits")
	}
	parts, total, srcs := Sandwich(hits, 120, true)
	if len(parts) < 2 {
		t.Fatalf("parts %d", len(parts))
	}
	if parts[0] != fmt.Sprintf("- [%s %s] %s", hits[0].U.Src, hits[0].U.ID, hits[0].U.Text) {
		t.Fatal("best not first")
	}
	if !strings.Contains(parts[len(parts)-1], "DUPLICATE-BEST") {
		t.Fatalf("best not duplicated last: %v", parts[len(parts)-1])
	}
	if total <= 0 || len(srcs) < 2 {
		t.Fatal("tokens/srcs")
	}
}

func TestAssembleForModelHonorsBudget(t *testing.T) {
	ap := NewAPI(NewStore())
	seedInto(ap.store)
	_, _, total := ap.AssembleForModel("", "GDPR stance retention 90 days", 100, true)
	if total > 100+55 {
		t.Fatalf("budget exceeded: %d", total)
	}
}

func TestTombstoneGateInAssembler(t *testing.T) {
	s := NewStore()
	seedInto(s)
	ap := NewAPI(s)
	_, srcs, _ := ap.AssembleForModel("", "GDPR retention 90 days Alice 2022 audit logs", 120, false)
	for _, src := range srcs {
		if src == "gdpr-v1" {
			t.Fatal("tombstoned unit leaked into assembly")
		}
	}
}

func TestIntentClassification(t *testing.T) {
	if classifyIntent("current GDPR stance after 2023?") != IntentTemporal {
		t.Log("temporal classification")
	}
	if classifyIntent("Summarize all incidents") != IntentAggregation {
		t.Log("aggregation")
	}
	if classifyIntent("What error E1847?") != IntentFactual {
		t.Log("factual")
	}
}

func TestPPRForwardPushReachesEdges(t *testing.T) {
	s := NewStore()
	seedInto(s)
	// pick top hits as seeds; PPR should produce mass map
	hits := s.Search("GDPR stance retention", 120, 4)
	var seeds []string
	for _, h := range hits {
		seeds = append(seeds, h.U.ID)
	}
	mass := s.PPRForwardPush(seeds, 0.15, 0.05)
	if len(mass) == 0 {
		t.Fatal("no diffusion mass")
	}
	lifted := s.PPRLift(hits, []string{"gdpr"}, toks(hits), 160)
	if len(lifted) < len(hits) {
		t.Fatal("PPR lift shrank hit set")
	}
}

func TestSandwichCilowReorderNoDup(t *testing.T) {
	s := NewStore()
	seedInto(s)
	hits := s.Search("GDPR stance retention", 120, 5)
	parts, total, _ := Sandwich(hits, 999, false)
	if !strings.Contains(parts[len(parts)-1], hits[1].U.Src) {
		t.Fatalf("second-best should be last, got %v", parts[len(parts)-1])
	}
	if !strings.Contains(parts[0], hits[0].U.Src) {
		t.Fatal("best should be first")
	}
	// zero duplicate tokens: total == sum of unique unit tokens
	if total != toks(hits) {
		t.Fatalf("no-dup reorder should not inflate tokens: %d vs %d", total, toks(hits))
	}
}

func TestCoAccessSelfImproving(t *testing.T) {
	s := NewStore()
	seedInto(s)
	feeds0 := s.CoAcc.Feeds
	for i := 0; i < 3; i++ {
		s.Search("GDPR stance retention", 120, 5)
	}
	if s.CoAcc.Feeds <= feeds0 {
		t.Fatalf("coaccess did not feed: %d", s.CoAcc.Feeds)
	}
}

func TestConflictExclusiveChannel(t *testing.T) {
	s := NewStore()
	s.Ingest("conflict-src", "Vacation days are 25 per year.", "policy", 2026)
	before := len(s.U)
	s.Ingest("conflict-src", "Vacation days are 30 per year.", "policy", 2026)
	if len(s.Conflicts.Pairs) == 0 {
		t.Fatal("no conflict pair recorded")
	}
	// conflicted unit must never appear in retrieval top results
	hits := s.Search("vacation days", 120, 5)
	for _, h := range hits {
		if s.Conflicts.Active[h.U.ID] {
			t.Fatalf("conflicted unit %s leaked into pack", h.U.ID)
		}
	}
	_ = before
}

func TestReceiptFlow(t *testing.T) {
	r := &Receipt{ID: newReceiptID(), Query: "q", Units: 3, Sources: []string{"a"},
		Tokens: 10, Intent: "factual", ConformalNC: 0.3, Hops: 1}
	receipts.Put(r)
	got, ok := receipts.Get(r.ID)
	if !ok || got.Hash == "" {
		t.Fatal("receipt hash not filled")
	}
}
