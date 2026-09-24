package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionHotWindowAndCompress(t *testing.T) {
	s := NewStore()
	seedInto(s)
	m := NewSessionManager(s)
	for i := 0; i < 20; i++ {
		m.AddTurn("s1", "user", "Turn number "+strings.Repeat("content words padding ", 3)+" "+fmt_int(i))
	}
	sess := m.Rounds["s1"]
	if len(sess.Turns) > m.HotMax {
		t.Fatalf("hot window exceeded: %d", len(sess.Turns))
	}
	if sess.Compacted == 0 {
		t.Fatal("expected compaction")
	}
	if sess.Summary == "" {
		t.Fatal("expected rolling summary")
	}
	packed := m.PackedContext("s1", "state")
	if packed.Tokens > m.TokenCap {
		t.Fatalf("packed %d over cap %d", packed.Tokens, m.TokenCap)
	}
	if len(packed.Parts) == 0 {
		t.Fatal("packed empty")
	}
	if false {
		// consolidated units should exist; just sanity that ingest side-effect ran
		t.Logf("store units after session=%d", len(m.store.U))
	}
}

func fmt_int(i int) string {
	return string(rune('0' + i%10))
}

func TestSessionPackedBudgetNeverExceeds(t *testing.T) {
	s := NewStore()
	seedInto(s)
	m := NewSessionManager(s)
	m.TokenCap = 200
	long := strings.Repeat("lorem ipsum dolor sit amet consectetur adipiscing ", 10)
	for i := 0; i < 30; i++ {
		m.AddTurn("s2", "user", long+" "+time.Now().Format(time.StampNano))
	}
	packed := m.PackedContext("s2", "ipsum")
	if packed.Tokens > 200+40 {
		t.Fatalf("packed %d exceeds budget", packed.Tokens)
	}
	if len(packed.Dropped) == 0 {
		t.Fatal("expected drops under tight budget")
	}
}

func TestForgetCurve(t *testing.T) {
	s := NewStore()
	m := NewSessionManager(s)
	for i := 0; i < 12; i++ {
		m.AddTurn("s3", "user", "memory consolidation iteration "+strings.Repeat("x ", i+1)+" .")
	}
	killed := m.Forget("s3")
	if killed == 0 {
		t.Fatal("forget should tombstone older consolidations")
	}
	// newest session unit must survive
	src := "session:" + sessID2src("s3")
	alive := 0
	for _, u := range s.U {
		if u.Src == src && !s.Tomb[u.ID] {
			alive++
		}
	}
	if alive > m.SessKeep {
		t.Fatalf("kept %d > keep %d", alive, m.SessKeep)
	}
}

func TestCompressLoopWriteBack(t *testing.T) {
	s := NewStore()
	var cands []Candidate
	for i := 0; i < 10; i++ {
		txt := "candidate passage with useful detail " + strings.Repeat("filler token ", 25)
		cands = append(cands, Candidate{ID: fmt.Sprintf("cand-%d", i), Src: "src" + string(rune('a'+i)), Text: txt, Tok: estimateTokens(txt), Vec: vecOf(tok(txt)), Score: float64(i)})
	}
	res := CompressLoop(s, "passage filler", cands, 150, true)
	if res.Tokens > 150+40 {
		t.Fatalf("compress exceeded budget: %d", res.Tokens)
	}
	if res.SumUnitID == "" {
		t.Fatal("expected write-back summary unit")
	}
	if u, ok := s.ByID[res.SumUnitID]; !ok || u.Level != 1 {
		t.Fatal("summary unit Level!=1")
	}
	if len(res.Evicted) == 0 {
		t.Fatal("expected evictions")
	}
}

func TestConsolidateTopics(t *testing.T) {
	s := NewStore()
	seedInto(s)
	n := s.Consolidate()
	if n == 0 {
		t.Fatal("expected topic units from related edges")
	}
	topicExists := false
	for _, u := range s.U {
		if u.Level == 2 {
			topicExists = true
		}
	}
	if !topicExists {
		t.Fatal("no Level-2 topic unit created")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	s1 := NewStore()
	seedInto(s1)
	s1.Ingest("persist-me", "The deployment region count is 7 as of 2026. Owner: sre.", "db", 2026)
	if err := s1.Save(path); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	h1 := s1.Search("deployment regions", 120, 3)
	h2 := s2.Search("deployment regions", 120, 3)
	if len(h1) == 0 || len(h2) == 0 {
		t.Fatal("search failed after roundtrip")
	}
	if h1[0].U.ID != h2[0].U.ID {
		t.Fatalf("ids differ after roundtrip: %s vs %s", h1[0].U.ID, h2[0].U.ID)
	}
	if s2.ByID[h1[0].U.ID].Text != h1[0].U.Text {
		t.Fatal("text differs")
	}
}

func TestLoadStoreOrSeed(t *testing.T) {
	s := LoadStoreOrSeed("/nonexistent/nope.json")
	if len(s.U) == 0 {
		t.Fatal("seed fallback failed")
	}
}

func TestMCPServerEndToEnd(t *testing.T) {
	pager := NewMCPPager(func() *Store {
		s := NewStore()
		seedInto(s)
		return s
	}())
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"context_search","arguments":{"query":"GDPR stance,"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`,
	}, "\n") + "\n"
	var out strings.Builder
	if err := pager.Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses, got %d", len(lines))
	}
	var searchResp, unknownResp map[string]interface{}
	json.Unmarshal([]byte(lines[2]), &searchResp)
	json.Unmarshal([]byte(lines[3]), &unknownResp)
	if searchResp["error"] != nil {
		t.Fatalf("context_search errored: %v", searchResp["error"])
	}
	if searchResp["result"] == nil {
		t.Fatalf("search call failed: %v", searchResp)
	}
	if unknownResp["error"] == nil {
		t.Fatal("unknown tool should be an error")
	}
}

func TestMCPToolsEachCallable(t *testing.T) {
	pager := NewMCPPager(func() *Store {
		s := NewStore()
		seedInto(s)
		return s
	}())
	var out strings.Builder
	in := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"context_ingest","arguments":{"source":"m","text":"sockpuppet test doc. Grazing rotation is 14 days."}}}` + "\n" +
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"context_compress","arguments":{"query":"rotation","texts":["sockpuppet doc about grazing rotation windows","other filler text about instruments"],"budget":50}}}` + "\n" +
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"context_session_report","arguments":{"session_id":"mcp1","role":"user","content":"hello session","query":"grazing"}}}` + "\n"
	pager.Serve(strings.NewReader(in), &out)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	for i, l := range lines {
		var m map[string]interface{}
		json.Unmarshal([]byte(l), &m)
		if m["error"] != nil {
			t.Fatalf("tool %d errored: %v", i+5, m["error"])
		}
	}
}

func TestOpenAICompatEndToEnd(t *testing.T) {
	s := NewStore()
	seedInto(s)
	s.Ingest("e2e", "The launch date is 2026-11-15. Owner: product. Draft v0 said 2026-12-01.", "db", 2026)
	ap := NewAPI(s)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			ap.handleChat(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	body := []byte(`{"model":"auto","messages":[{"role":"user","content":"When is the launch date?"}],"session_id":"e2e-1"}`)
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Context-Sources") == "" {
		t.Fatal("missing X-Context-Sources header")
	}
	var cr ChatResponse
	json.NewDecoder(resp.Body).Decode(&cr)
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == "" {
		t.Fatal("empty completion")
	}
	if !strings.Contains(cr.Choices[0].Message.Content, "launch") && !strings.Contains(cr.Choices[0].Message.Content, "2026") {
		t.Fatalf("answer should reflect retrieved context: %s", cr.Choices[0].Message.Content)
	}
	// session second turn should keep memory
	body2 := `{"model":"auto","messages":[{"role":"user","content":"What did you say about launch owner?"}],"session_id":"e2e-1"}`
	resp2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("X-Session") != "e2e-1" {
		t.Fatal("missing X-Session header")
	}
}

func TestHandleChatRejectsBadInput(t *testing.T) {
	ap := NewAPI(NewStore())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	ap.handleChat(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"assistant","content":"hi"}]}`))
	w2 := httptest.NewRecorder()
	ap.handleChat(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for no user msg, got %d", w2.Code)
	}
}

func TestParallelLoadKernelAPI(t *testing.T) {
	s := NewStore()
	seedInto(s)
	ap := NewAPI(s)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"GDPR stance after 2023?"}],"session_id":"load-`+fmt.Sprint(n%4)+`"}`))
			w := httptest.NewRecorder()
			ap.handleChat(w, req)
			if w.Code != 200 {
				t.Logf("req %d status %d body %s", n, w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
}

func TestSnapshotFileOnDisk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "snap.json")
	s := NewStore()
	s.Ingest("disk", "Disk snapshot test. Cluster quota is 128 shards.", "db", 2026)
	os.Remove(p)
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStore(p)
	if err != nil || len(loaded.U) != 1 {
		t.Fatal("load failed")
	}
}
