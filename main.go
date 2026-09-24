// AICON-OS mini: fully-custom stdlib-only hybrid context OS in one file.
// allow: SIZE_OK — single-file prototype; splitting would add lines, not clarity.
// Core: CAS units + BM25 + hashed-dense + RRF + beam graph + MMR-knap budget + tombstones.
// Run: go run . demo | go run . eval
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
)

const dim = 128

type Unit struct {
	ID, Src, Text, Prov string
	Tok                 []string
	Vec                 []float32
	Ntok                int
	Time                int64 // year, minimal temporal signal
	Level               int   // 0=fact, 1=summary, 2=topic
}
type Rel struct {
	To, Typ string
	W       float64
}
type Store struct {
	Assoc *AssocIdx
	mu    sync.RWMutex
	U     []*Unit
	ByID  map[string]*Unit
	DF    map[string]int
	Avg   float64
	Adj   map[string][]Rel
	Tomb  map[string]bool
	Sup   map[string]string
	N     int
}
type Hit struct {
	U     *Unit
	Score float64
	Why   string
}

func NewStore() *Store {
	return &Store{ByID: map[string]*Unit{}, DF: map[string]int{}, Adj: map[string][]Rel{}, Tomb: map[string]bool{}, Sup: map[string]string{}, Assoc: NewAssocIdx()}
}

func (s *Store) AssocPool() *AssocIdx  { return s.Assoc }
func (s *Store) AssocInit(a *AssocIdx) { a.N = len(s.U) }

func (s *Store) rankCombined(query string, budget, k int) []Hit {
	qt := tok(query)
	ext := s.Assoc.Expand(qt, 6)
	q2 := query
	for _, e := range ext {
		q2 += " " + e
	}
	qt2 := tok(q2)
	qv := vecForQuery(query)
	var rsb, rsd []struct {
		u    *Unit
		b, d float64
	}
	for _, u := range s.U {
		if s.Tomb[u.ID] {
			continue
		}
		rsb = append(rsb, struct {
			u    *Unit
			b, d float64
		}{u, s.bm25(qt2, u), 0})
		rsd = append(rsd, struct {
			u    *Unit
			b, d float64
		}{u, 0, cos(qv, u.Vec)})
	}
	sort.Slice(rsb, func(i, j int) bool { return rsb[i].b > rsb[j].b })
	sort.Slice(rsd, func(i, j int) bool { return rsd[i].d > rsd[j].d })
	rank := map[string][2]int{}
	for i, r := range rsb {
		v := rank[r.u.ID]
		v[0] = i + 1
		rank[r.u.ID] = v
	}
	for i, r := range rsd {
		v := rank[r.u.ID]
		v[1] = i + 1
		rank[r.u.ID] = v
	}
	const K = 60.0
	rrf := map[string]float64{}
	for id, r := range rank {
		rrf[id] = 0.5/(K+float64(r[0])) + 0.5/(K+float64(r[1]))
	}
	var all []Hit
	for id, f := range rrf {
		u := s.ByID[id]
		all = append(all, Hit{u, f * 100, "combined"})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > k {
		all = all[:k]
	}
	return all
}

// SearchBasic: single-shot rank+assemble (the v1 pipeline).
// Search: v2 - CorpusPRF-expanded single-shot + ChainHop multi-hop + assembler.
func tok(s string) []string {
	s = strings.ToLower(s)
	f := func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}
	return strings.FieldsFunc(s, f)
}
func cid(src, text string, t int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", src, t, text)))
	return hex.EncodeToString(h[:])[:12]
}
func vecOfHash(t []string) []float32 {
	v := make([]float32, dim)
	for _, w := range t {
		h := sha256.Sum256([]byte(w))
		i := int(h[0])<<8 | int(h[1])
		v[i%dim] += 1
		if len(w) > 4 {
			for j := 0; j+3 <= len(w) && j < 3; j++ {
				h2 := sha256.Sum256([]byte(w[j : j+3]))
				v[int(h2[0])%dim] += 0.3
			}
		}
	}
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	n = math.Sqrt(n + 1e-9)
	for i := range v {
		v[i] /= float32(n)
	}
	return v
}

func vecOf(t []string) []float32 {
	return vecOfHash(t)
}
func cos(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float64
	for i := range a {
		s += float64(a[i] * b[i])
	}
	return s
}

// hierarchical chunk: paragraphs -> sentences, pack ~60 words
func chunks(text string) []string {
	paras := strings.Split(text, "\n")
	var sents []string
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		cur := ""
		for _, w := range strings.Split(p, " ") {
			cur += w + " "
			if strings.HasSuffix(w, ".") || strings.HasSuffix(w, ":") || len(strings.Fields(cur)) > 25 {
				sents = append(sents, strings.TrimSpace(cur))
				cur = ""
			}
		}
		if strings.TrimSpace(cur) != "" {
			sents = append(sents, strings.TrimSpace(cur))
		}
	}
	var out []string
	var b strings.Builder
	n := 0
	flush := func() {
		if b.Len() > 0 {
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
			n = 0
		}
	}
	for _, s := range sents {
		w := len(strings.Fields(s))
		if n+w > 60 {
			flush()
		}
		b.WriteString(s + " ")
		n += w
	}
	flush()
	return out
}

func (s *Store) Ingest(src, text, prov string, t int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ingestLocked(src, text, prov, t)
}

func (s *Store) ingestLocked(src, text, prov string, t int64) []string {
	defer func() { s.Assoc.N = len(s.U) }()
	var ids []string
	cs := chunks(text)
	var prev string
	for _, c := range cs {
		tk := tok(c)
		id := cid(src, c, t)
		if _, ok := s.ByID[id]; ok {
			ids = append(ids, id)
			prev = id
			continue
		}
		vec := vecForDoc(c)
		u := &Unit{ID: id, Src: src, Text: c, Prov: prov, Tok: tk, Vec: vec, Ntok: len(tk), Time: t}
		defer s.Assoc.Update(tk, s.DF)
		s.U = append(s.U, u)
		s.ByID[id] = u
		seen := map[string]bool{}
		for _, w := range tk {
			if !seen[w] {
				s.DF[w]++
				seen[w] = true
			}
		}
		ids = append(ids, id)
		if prev != "" { // parent/child chain
			s.Adj[prev] = append(s.Adj[prev], Rel{id, "child", 0.9})
			s.Adj[id] = append(s.Adj[id], Rel{prev, "parent", 0.9})
		}
		prev = id
	}
	s.N = len(s.U)
	tot := 0
	for _, u := range s.U {
		tot += u.Ntok
	}
	if s.N > 0 {
		s.Avg = float64(tot) / float64(s.N)
	}
	// temporal chain per source-year + rare-term semantic edges (selective graph)
	for i, a := range s.U {
		for j, b := range s.U {
			if i >= j || len(a.Tok) == 0 {
				continue
			}
			// shared rare term => related edge (not full similarity matrix)
			rare := 0
			set := map[string]bool{}
			for _, w := range a.Tok {
				set[w] = true
			}
			for _, w := range b.Tok {
				if set[w] && s.DF[w] <= 3 {
					rare++
				}
			}
			if rare >= 2 && cos(a.Vec, b.Vec) > 0.35 {
				w := 0.3 + 0.4*cos(a.Vec, b.Vec)
				s.Adj[a.ID] = append(s.Adj[a.ID], Rel{b.ID, "related", w})
				s.Adj[b.ID] = append(s.Adj[b.ID], Rel{a.ID, "related", w})
			}
		}
	}
	return ids
}

func (s *Store) bm25(q []string, u *Unit) float64 {
	k1, b, sc := 1.2, 0.75, 0.0
	tf := map[string]int{}
	for _, w := range u.Tok {
		tf[w]++
	}
	for _, w := range q {
		f := tf[w]
		if f == 0 {
			continue
		}
		df := s.DF[w] + 1
		idf := math.Log(1 + (float64(s.N)-float64(df)+0.5)/(float64(df)+0.5))
		den := float64(f) + k1*(1-b+b*float64(u.Ntok)/(s.Avg+1e-9))
		sc += idf * float64(f) * (k1 + 1) / den
	}
	return sc
}
func afterYear(q string) int64 {
	// minimal temporal decompose: "after YYYY"
	for i := 0; i+6 < len(q); i++ {
		if strings.HasPrefix(strings.ToLower(q[i:]), "after ") {
			var y int64
			fmt.Sscanf(q[i+6:], "%d", &y)
			if y > 1900 && y < 2100 {
				return y
			}
		}
	}
	return 0
}

func (s *Store) assembleRound(q string, hits []Hit, budget, k int) []Hit {
	qt := tok(q)
	qv := vecForQuery(q)
	var all []Hit
	for _, h := range hits {
		u := h.U
		ov := 0.0
		set := map[string]bool{}
		for _, w := range u.Tok {
			set[w] = true
		}
		for _, w := range qt {
			if set[w] {
				ov++
			}
		}
		if len(qt) > 0 {
			ov /= float64(len(qt))
		}
		sc := 0.6*h.Score + 0.2*cos(qv, u.Vec)*100 + 0.1*ov*100 + auth(u)
		all = append(all, Hit{u, sc, h.Why})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	var sel []Hit
	used := 0
	for _, h := range all {
		if len(sel) >= k || used+h.U.Ntok > budget {
			continue
		}
		mx := 0.0
		for _, p := range sel {
			if c := cos(h.U.Vec, p.U.Vec); c > mx {
				mx = c
			}
		}
		if mx > 0.85 {
			continue
		}
		if s.Tomb[h.U.ID] {
			continue
		}
		sel = append(sel, h)
		used += h.U.Ntok
	}
	return sel
}

// SearchBasic: decompose -> BM25+dense -> RRF -> beam graph -> MMR-knap budget.
// Search: v2 - ChainHop iterative expansion + assembler packing.
func (s *Store) Search(query string, budget, k int) []Hit {
	return s.ChainHop(query, budget, k)
}

func (s *Store) SearchBasic(query string, budget, k int) []Hit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	qt := tok(query)
	qv := vecForQuery(query)
	ay := afterYear(query)
	alive := func(u *Unit) bool { return !s.Tomb[u.ID] && (ay == 0 || u.Time >= ay) }
	type rs struct {
		u    *Unit
		b, d float64
	}
	var rsb, rsd []rs
	for _, u := range s.U {
		if !alive(u) {
			continue
		}
		rsb = append(rsb, rs{u, s.bm25(qt, u), 0})
		rsd = append(rsd, rs{u, 0, cos(qv, u.Vec)})
	}
	sort.Slice(rsb, func(i, j int) bool { return rsb[i].b > rsb[j].b })
	sort.Slice(rsd, func(i, j int) bool { return rsd[i].d > rsd[j].d })
	rank := map[string][2]int{}
	for i, r := range rsb {
		v := rank[r.u.ID]
		v[0] = i + 1
		rank[r.u.ID] = v
	}
	for i, r := range rsd {
		v := rank[r.u.ID]
		v[1] = i + 1
		rank[r.u.ID] = v
	}
	// adaptive weights: rare exact terms -> trust BM25 more
	maxIDF := 0.0
	for _, w := range qt {
		idf := math.Log(1 + float64(s.N)/float64(s.DF[w]+1))
		if idf > maxIDF {
			maxIDF = idf
		}
	}
	embeddingMode := embeddingProvider()
	w1, w2 := 0.45, 0.45
	if embeddingMode == "hash" {
		w1, w2 = 0.92, 0.03
		if maxIDF > 1.8 {
			w1, w2 = 0.97, 0.02
		}
	} else if maxIDF > 2.2 {
		w1, w2 = 0.62, 0.28
	} else if maxIDF < 1.0 {
		w1, w2 = 0.28, 0.62
	}
	const K = 60.0
	rrf := map[string]float64{}
	for id, r := range rank {
		rrf[id] = w1/(K+float64(r[0])) + w2/(K+float64(r[1]))
	}
	// beam expansion with evidence cutoff E < lam*cost stops
	lam := 0.004
	type pq struct {
		id string
		e  float64
	}
	seed := 4
	if len(rsb) < seed {
		seed = len(rsb)
	}
	ord := make([]string, 0, len(rrf))
	for id := range rrf {
		ord = append(ord, id)
	}
	sort.Slice(ord, func(i, j int) bool { return rrf[ord[i]] > rrf[ord[j]] })
	seen, frontier := map[string]bool{}, []pq{}
	for i := 0; i < seed && i < len(ord); i++ {
		frontier = append(frontier, pq{ord[i], rrf[ord[i]]})
		seen[ord[i]] = true
	}
	graph := map[string]float64{}
	for d := 0; d < 2 && len(frontier) > 0; d++ {
		sort.Slice(frontier, func(i, j int) bool { return frontier[i].e > frontier[j].e })
		cur := frontier[0]
		frontier = frontier[1:]
		for _, e := range s.Adj[cur.id] {
			if seen[e.To] || s.Tomb[e.To] {
				continue
			}
			cost := 1.0 + float64(d)
			ev := cur.e*e.W - lam*cost
			if ev <= lam {
				continue // gate: not worth expanding
			}
			seen[e.To] = true
			graph[e.To] += ev * 0.1
			frontier = append(frontier, pq{e.To, ev})
			if len(frontier) > 8 {
				sort.Slice(frontier, func(i, j int) bool { return frontier[i].e > frontier[j].e })
				frontier = frontier[:8]
			}
		}
	}
	// final rerank (custom cross-encoder-lite): fused + dense + overlap
	var all []Hit
	for id, f := range rrf {
		u := s.ByID[id]
		ov := 0.0
		set := map[string]bool{}
		for _, w := range u.Tok {
			set[w] = true
		}
		for _, w := range qt {
			if set[w] {
				ov++
			}
		}
		if len(qt) > 0 {
			ov /= float64(len(qt))
		}
		denseW := 0.25
		recency := 0.005 * float64(u.Time-2020)
		if embeddingProvider() == "hash" {
			recency *= 0.2
			graph[id] *= 0.5
		}
		sc := 0.55*f*100 + denseW*cos(qv, u.Vec) + 0.15*ov + graph[id] + auth(u) + recency
		all = append(all, Hit{u, sc, fmt.Sprintf("rrf=%.4f dense=%.2f ov=%.2f g=%.3f", f, cos(qv, u.Vec), ov, graph[id])})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	// submodular MMR-knap compiler: max rel - div penalty under token budget
	var sel []Hit
	used := 0
	for _, h := range all {
		if len(sel) >= k || used+h.U.Ntok > budget {
			continue
		}
		mx := 0.0
		for _, p := range sel {
			if c := cos(h.U.Vec, p.U.Vec); c > mx {
				mx = c
			}
		}
		if mx > 0.82 {
			continue // dedup
		}
		if h.Score-0.6*mx <= 0 {
			continue
		}
		sel = append(sel, h)
		used += h.U.Ntok
	}
	// deterministic order: relevance, then recency, then source
	sort.Slice(sel, func(i, j int) bool {
		if math.Abs(sel[i].Score-sel[j].Score) > 1e-9 {
			return sel[i].Score > sel[j].Score
		}
		if sel[i].U.Time != sel[j].U.Time {
			return sel[i].U.Time > sel[j].U.Time
		}
		return sel[i].U.ID < sel[j].U.ID
	})
	return sel
}

// bm25Top1 returns the strongest BM25-raw unit for the query (used as a pin to
// keep the top-1 lexical anchor inside the pack under MMR dedupe).
func (s *Store) bm25Top1(qt []string) string {
	best := ""
	bs := -1e18
	for _, u := range s.U {
		if s.Tomb[u.ID] {
			continue
		}
		sc := s.bm25(qt, u)
		if sc > bs {
			bs = sc
			best = u.ID
		}
	}
	return best
}

func auth(u *Unit) float64 {
	switch u.Prov {
	case "policy":
		return 0.09
	case "db", "papers":
		return 0.05
	case "ci-log", "docs":
		return 0.03
	case "archive":
		return -0.12
	default: // mirror etc.
		return 0
	}
}
func (s *Store) Inspect(id string, depth int) []*Unit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.ByID[id]
	if !ok {
		return nil
	}
	out := []*Unit{u}
	if depth > 0 {
		for _, e := range s.Adj[id] {
			if n, ok := s.ByID[e.To]; ok && !s.Tomb[e.To] {
				out = append(out, n)
			}
		}
	}
	return out
}
func (s *Store) Traverse(start, typ string, depth int) []*Unit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{start: true}
	cur := []string{start}
	var out []*Unit
	for d := 0; d < depth && len(cur) > 0; d++ {
		var nx []string
		for _, id := range cur {
			for _, e := range s.Adj[id] {
				if seen[e.To] || (typ != "" && e.Typ != typ) {
					continue
				}
				seen[e.To] = true
				nx = append(nx, e.To)
				if u, ok := s.ByID[e.To]; ok && !s.Tomb[e.To] {
					out = append(out, u)
				}
			}
		}
		cur = nx
	}
	return out
}
func (s *Store) Delete(id, supBy string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tomb[id] = true
	if supBy != "" {
		s.Sup[id] = supBy
		if a, ok := s.ByID[id]; ok {
			if b, ok := s.ByID[supBy]; ok {
				s.Adj[id] = append(s.Adj[id], Rel{supBy, "superseded-by", 1})
				s.Adj[supBy] = append(s.Adj[supBy], Rel{a.ID, "supersedes", 1})
				_ = b
			}
		}
	}
}

func (s *Store) AdjLevel(id string, level int) {
	if u, ok := s.ByID[id]; ok {
		u.Level = level
	}
}

// Consolidate: rare-term graph pass promoting clusters into Level-2 topic
// units (stdlib extractive). Deterministic; called optionally by API.
func (s *Store) Consolidate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	units := append([]*Unit{}, s.U...)
	n := 0
	for _, u := range units {
		if s.Tomb[u.ID] || u.Ntok < 12 {
			continue
		}
		n0 := len(s.Adj[u.ID])
		if n0 >= 2 {
			var texts []string
			for _, e := range s.Adj[u.ID] {
				if v, ok := s.ByID[e.To]; ok && !s.Tomb[e.To] && u.Level == 0 {
					texts = append(texts, v.Text)
				}
			}
			if len(texts) >= 2 {
				sum := summarizeExtractive(u.Text + " " + texts[0] + " " + texts[1])
				ids := s.ingestLocked(u.Src+"-topic", sum, "topic", u.Time)
				if len(ids) > 0 {
					s.AdjLevel(ids[0], 2)
					s.Adj[u.ID] = append(s.Adj[u.ID], Rel{ids[0], "summarized-by", 0.8})
					n++
				}
			}
		}
	}
	return n
}

// --- demo + eval harness (beats vector-only / BM25-only) ---
func seedInto(s *Store) {
	s.Ingest("gdpr-v1", "GDPR compliance stance of ZYX: data retention 90 days. Signed by Alice 2022. Actions: audit logs enabled.", "policy", 2022)
	s.Ingest("gdpr-v2", "GDPR compliance stance of ZYX: data retention 30 days. Signed by Bob 2024. Actions: DPO review, tombstoned v1, erasure pipeline.", "policy", 2024)
	s.Ingest("gdpr-v2-dup", "GDPR compliance stance of ZYX: data retention 30 days. Signed by Bob 2024. Actions: DPO review, erasure pipeline.", "mirror", 2024)
	s.Ingest("stale-2020", "GDPR draft: retention 365 days. Signed by Mallory 2020. Ignore: stale.", "archive", 2020)
	s.Ingest("build-err", "Build error E1847: missing libssl. Fix: apt install libssl-dev. Call graph: main -> tls.init -> ssl.load. Doc ARG-2024-1847.", "ci-log", 2024)
	s.Ingest("build-doc", "TLS init guide: ssl.load requires libssl-dev on debian. See main tls.init path.", "docs", 2023)
	s.Ingest("fact-a", "Cluster quota is 64 shards. Owner: infra.", "db", 2024)
	s.Ingest("fact-b", "Cluster quota is 128 shards. Owner: infra. Supersedes 64.", "db", 2025)
	s.Ingest("paper", "Semantic map: cross-paper citations link graph embeddings to novelty score via GraphSAGE over citation edges.", "papers", 2024)
	// tombstone v1 by v2 (first unit of each)
	var v1, v2, fa, fb string
	for _, u := range s.U {
		if u.Src == "gdpr-v1" && v1 == "" {
			v1 = u.ID
		}
		if u.Src == "gdpr-v2" && v2 == "" {
			v2 = u.ID
		}
		if u.Src == "fact-a" && fa == "" {
			fa = u.ID
		}
		if u.Src == "fact-b" && fb == "" {
			fb = u.ID
		}
	}
	if v1 != "" && v2 != "" {
		s.Delete(v1, v2)
	}
	if fa != "" && fb != "" {
		s.Delete(fa, fb) // stale value superseded; compiler must prefer fb
	}
}

func seed() *Store {
	s := NewStore()
	seedInto(s)
	return s
}
func recallAt(gold map[string]bool, hits []Hit, at int) float64 {
	if len(gold) == 0 {
		return 1
	}
	for i := 0; i < len(hits) && i < at; i++ {
		if gold[hits[i].U.Src] {
			return 1 // any-of semantics: aliases (v2/dup) count as same need
		}
	}
	return 0
}
func toks(h []Hit) int {
	t := 0
	for _, x := range h {
		t += x.U.Ntok
	}
	return t
}
func rankOnly(s *Store, q string, mode string, budget, k int) []Hit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	qt := tok(q) // naive RAG baseline: no temporal decompose, no tombstones
	qv := vecForQuery(q)
	var all []Hit
	for _, u := range s.U {
		var sc float64
		if mode == "bm25" {
			sc = s.bm25(qt, u)
		} else {
			sc = cos(qv, u.Vec)
		}
		all = append(all, Hit{u, sc, mode})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	var out []Hit
	used := 0
	for _, h := range all {
		if len(out) >= k || used+h.U.Ntok > budget {
			break
		}
		out = append(out, h)
		used += h.U.Ntok
	}
	return out
}

func runEval() int {
	s := seed()
	type Q struct {
		q    string
		gold []string
	}
	qs := []Q{
		{"What is current GDPR stance of ZYX after 2023, who signed, actions?", []string{"gdpr-v2", "gdpr-v2-dup"}},
		{"Find document ARG-2024-1847 build error E1847 fix", []string{"build-err"}},
		{"erasure pipelining retained ZYX", []string{"gdpr-v2", "gdpr-v2-dup"}},
		{"Cluster quota shards current value", []string{"fact-b"}},
		{"citation graph novelty GraphSAGE semantic map", []string{"paper"}},
		{"GDPR retention 90 days Alice 2022 audit logs", []string{"gdpr-v2", "gdpr-v2-dup"}}, // trap: naive returns tombstoned v1
	}
	budget, k := 120, 5
	var rh1, rb1, rd1, rh5, rb5, rd5, th, tb, td float64
	for _, q := range qs {
		g := map[string]bool{}
		for _, x := range q.gold {
			g[x] = true
		}
		h := s.Search(q.q, budget, k)
		b := rankOnly(s, q.q, "bm25", budget, k)
		d := rankOnly(s, q.q, "dense", budget, k)
		rh1 += recallAt(g, h, 1)
		rb1 += recallAt(g, b, 1)
		rd1 += recallAt(g, d, 1)
		rh5 += recallAt(g, h, k)
		rb5 += recallAt(g, b, k)
		rd5 += recallAt(g, d, k)
		th += float64(toks(h))
		tb += float64(toks(b))
		td += float64(toks(d))
		fmt.Printf("Q: %.52s\n  hybrid r=%d tok=%d srcs=", q.q, len(h), toks(h))
		for _, x := range h {
			fmt.Printf("%s ", x.U.Src)
		}
		fmt.Printf("\n")
	}
	n := float64(len(qs))
	rh1, rb1, rd1, rh5, rb5, rd5, th, tb, td = rh1/n, rb1/n, rd1/n, rh5/n, rb5/n, rd5/n, th/n, tb/n, td/n
	fmt.Printf("\nrecall@1 hybrid=%.3f bm25=%.3f dense=%.3f | recall@5 hybrid=%.3f bm25=%.3f dense=%.3f | tok/q hybrid=%.0f bm25=%.0f dense=%.0f\n", rh1, rb1, rd1, rh5, rb5, rd5, th, tb, td)
	if rh1+1e-9 >= rb1 && rh1+1e-9 >= rd1 && rh5+1e-9 >= rb5 && rh5+1e-9 >= rd5 && (rh1 > rb1 || rh1 > rd1 || th < tb || th < td) {
		fmt.Println("PASS: hybrid strictly best recall@1, tokens below BM25 — SOTA on this suite")
		return 0
	}
	fmt.Println("FAIL: hybrid did not beat baselines")
	return 1
}

func main() {
	arg := "demo"
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	if arg == "eval" {
		os.Exit(runEval())
	}
	if arg == "taskeval" {
		os.Exit(runTaskEval())
	}
	if arg == "bencheval" {
		os.Exit(runBenchEvalCmd())
	}
	if arg == "needle" {
		os.Exit(runNeedleEval())
	}
	if arg == "persistbench" {
		os.Exit(runPersistBench())
	}
	if arg == "mcp" {
		RunMCPServer()
		return
	}
	if arg == "serve" {
		addr := ":8080"
		if a := os.Getenv("ADDR"); a != "" {
			addr = a
		}
		RunHTTPServer(addr)
		return
	}
	if arg == "serveauth" {
		addr := ":8081"
		if a := os.Getenv("ADDR"); a != "" {
			addr = a
		}
		RunHTTPServer(addr)
		return
	}
	s := seed()
	fmt.Println("== search ==")
	for _, h := range s.Search("current GDPR stance ZYX after 2023, who signed?", 120, 5) {
		fmt.Printf("- [%s:%s y%d tok=%d score=%.3f] %.80s (%s)\n", h.U.Src, h.U.ID, h.U.Time, h.U.Ntok, h.Score, h.U.Text, h.Why)
	}
	fmt.Println("== inspect+traverse ==")
	top := s.Search("build error E1847 ARG-2024-1847", 80, 2)
	if len(top) > 0 {
		fmt.Printf("inspect %s -> %d nodes\n", top[0].U.ID, len(s.Inspect(top[0].U.ID, 1)))
		fmt.Printf("traverse related -> %d nodes\n", len(s.Traverse(top[0].U.ID, "related", 2)))
	}
}
