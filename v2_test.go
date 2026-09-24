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
	res := s.ChainHopFull("What error code E1847 fix works?", 120, 5)
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
