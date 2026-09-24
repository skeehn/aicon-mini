package main

// SessionManager: functional-infinite-context session layer.
// Layer 0: hot window (last N turns verbatim).
// Layer 1: rolling extractive summary (working memory).
// Layer 2: consolidated units auto-ingested into Store (episodic memory).
// Forget-curve: consolidated units untouched after K sessions get tombstoned
// (superseded by the newer consolidation) unless pinned.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Turn struct {
	Role      string // "user" | "assistant" | "system"
	Content   string
	Tokens    int
	At        time.Time
	SessionID string
}

type Session struct {
	mu        sync.Mutex
	ID        string
	Turns     []Turn
	HotMax    int // verbatim turns kept (Layer 0)
	TokenCap  int // total token budget for packed context
	Summary   string
	SumTok    int
	Compacted int // number of compression passes
	LastAt    time.Time
}

type SessionManager struct {
	mu        sync.Mutex
	Rounds    map[string]*Session
	HotMax    int
	TokenCap  int
	SessKeep  int // sessions a consolidated unit survives untouched
	compactor func(string) string
	store     *Store
	ingestedN int
}

func NewSessionManager(s *Store) *SessionManager {
	return &SessionManager{
		Rounds:    map[string]*Session{},
		HotMax:    8,
		TokenCap:  2048,
		SessKeep:  3,
		store:     s,
		compactor: summarizeExtractive,
	}
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return len(tok(s))
}

func summarizeExtractive(text string) string {
	// stdlib extractive: score sentences by rare-term density + positional priority.
	// first + last sentences get a bonus (boundary effect).
	sents := splitSentences(text)
	if len(sents) <= 2 {
		return text
	}
	d := len(sents)
	s0 := 0.18
	s1 := 0.5
	s2 := 0.32
	type sc struct {
		i int
		v float64
	}
	scores := make([]sc, len(sents))
	seen := map[string]int{}
	for _, sn := range sents {
		for _, w := range tok(sn) {
			seen[w]++
		}
	}
	maxFreq := 0
	for _, f := range seen {
		if f > maxFreq {
			maxFreq = f
		}
	}
	for i, sn := range sents {
		words := tok(sn)
		if len(words) == 0 {
			continue
		}
		density := 0.0
		for _, w := range words {
			density += float64(seen[w]) / float64(maxFreq)
		}
		density /= float64(len(words))
		pos := 1.0
		if i == 0 {
			pos = 1.6
		} else if i == len(sents)-1 {
			pos = 1.3
		} else if i < len(sents)/3 {
			pos = 1.15
		}
		lpos := 1.0
		lnorm := float64(i+1) / float64(d)
		if lnorm < 0.5 {
			lpos = 1.0 + lnorm
		} else {
			lpos = 2.0 - lnorm
		}
		scores[i] = sc{i, density * pos * lpos * (s0*1 + s1*1 + s2*1)}
	}
	// top sentences keep original order
	pick := len(sents) / 3
	if pick < 1 {
		pick = 1
	}
	if pick > 5 {
		pick = 5
	}
	idx := map[int]bool{}
	sortSc := append([]sc{}, scores...)
	for i := 0; i < len(sortSc); i++ {
		for j := i + 1; j < len(sortSc); j++ {
			if sortSc[j].v > sortSc[i].v {
				sortSc[i], sortSc[j] = sortSc[j], sortSc[i]
			}
		}
	}
	for i := 0; i < pick && i < len(sortSc); i++ {
		idx[sortSc[i].i] = true
	}
	var out []string
	for i := range sents {
		if idx[i] {
			out = append(out, sents[i])
		}
	}
	return strings.Join(out, " ")
}

func splitSentences(text string) []string {
	var out []string
	cur := strings.Builder{}
	for _, w := range strings.Split(text, " ") {
		cur.WriteString(w + " ")
		if strings.HasSuffix(w, ".") || strings.HasSuffix(w, "!") || strings.HasSuffix(w, "?") {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func (m *SessionManager) AddTurn(sessID, role, content string) *Session {
	m.mu.Lock()
	s := m.Rounds[sessID]
	if s == nil {
		s = &Session{ID: sessID, HotMax: m.HotMax, TokenCap: m.TokenCap}
		m.Rounds[sessID] = s
	}
	t := Turn{Role: role, Content: content, Tokens: estimateTokens(content), At: time.Now(), SessionID: sessID}
	s.Turns = append(s.Turns, t)
	s.LastAt = t.At
	m.maybeCompress(s)
	m.mu.Unlock()
	return s
}

func (m *SessionManager) maybeCompress(s *Session) bool {
	// hot window overflow: evict oldest verbatim turns into summary
	if len(s.Turns) <= s.HotMax {
		return false
	}
	evict := s.Turns[:len(s.Turns)-s.HotMax]
	var texts []string
	for _, t := range evict {
		texts = append(texts, t.Role+": "+t.Content)
	}
	joined := strings.Join(texts, "\n")
	newSum := summarizeExtractive(strings.TrimSpace(s.Summary + "\n" + joined))
	capTok := 192
	if s.TokenCap/4 > capTok {
		capTok = s.TokenCap / 4
	}
	s.Summary = truncateTokens(newSum, capTok)
	s.SumTok = estimateTokens(s.Summary)
	s.Turns = append([]Turn{}, s.Turns[len(s.Turns)-s.HotMax:]...)
	s.Compacted++
	// store consolidation occurs after lock release (stamped by flush)
	m.ingestE(s)
	return true
}

func (m *SessionManager) ingestE(s *Session) {
	if m.store == nil {
		return
	}
	ids := m.store.Ingest("session:"+sessID2src(s.ID), s.Summary, "session", int64(time.Now().Year()))
	m.ingestedN += len(ids)
}

func (m *SessionManager) Store() *Store { return m.store }

func sessID2src(id string) string {
	if len(id) > 24 {
		return id[:24]
	}
	return id
}

// PackedContext returns the current assembled context inside the token budget:
// [summary] + [hot turns longest-kept-last], evicting longest-relevant-first.
// Returns parts + total tokens + what was dropped.
type Packed struct {
	Parts   []string
	Tokens  int
	Dropped []string
}

func (m *SessionManager) PackedContext(sessID string, query string) Packed {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.Rounds[sessID]
	if s == nil {
		return Packed{}
	}
	parts := []string{}
	total := 0
	dropped := []string{}
	if s.Summary != "" {
		parts = append(parts, "[SUMMARY] "+s.Summary)
		total += s.SumTok
	}
	// rank hot turns by relevance to query (dense cosine, no network - hash)
	qv := vecOf(tok(query))
	turns := append([]Turn{}, s.Turns...)
	type tw struct {
		i   int
		v   float64
		len int
	}
	weights := make([]tw, len(turns))
	for i, t := range turns {
		weights[i] = tw{i, cos(qv, vecOf(tok(t.Content))), t.Tokens}
	}
	// pack most relevant first; if budget exceeded evict longest-relevant-first (lowest rel-per-token)
	budget := s.TokenCap - total
	if budget < 0 {
		budget = 0
	}
	order := make([]int, len(turns))
	for i := range order {
		order[i] = i
	}
	// sort by relevance desc
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if weights[order[j]].v > weights[order[i]].v {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	kept := map[int]bool{}
	for _, i := range order {
		if weights[i].len <= budget && len(kept) < m.HotMax {
			kept[i] = true
			budget -= weights[i].len
			total += weights[i].len
		}
	}
	// emit in chronological order
	for i := range s.Turns {
		if kept[i] {
			parts = append(parts, s.Turns[i].Role+": "+s.Turns[i].Content)
		} else {
			dropped = append(dropped, fmt.Sprintf("turn%d(%d tok)", i, s.Turns[i].Tokens))
		}
	}
	return Packed{Parts: parts, Tokens: total, Dropped: dropped}
}

// ForgetCurve: tombstone session-consolidated units older than SessKeep sessions
// (superseded by the newest consolidation of that session).
func (m *SessionManager) Forget(sessID string) int {
	if m.store == nil {
		return 0
	}
	n := 0
	src := "session:" + sessID2src(sessID)
	var ids []string
	for _, u := range m.store.U {
		if u.Src == src {
			ids = append(ids, u.ID)
		}
	}
	if len(ids) > m.SessKeep {
		keepAt := len(ids) - m.SessKeep
		for i := 0; i < keepAt; i++ {
			m.store.Delete(ids[i], ids[len(ids)-1])
			n++
		}
	}
	return n
}

func truncateTokens(text string, maxTok int) string {
	if maxTok <= 0 || estimateTokens(text) <= maxTok {
		return text
	}
	sents := splitSentences(text)
	var out []string
	n := 0
	for _, sn := range sents {
		t := estimateTokens(sn)
		if n+t > maxTok {
			break
		}
		out = append(out, sn)
		n += t
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ")
}

func (m *SessionManager) Stats() map[string]int {
	return map[string]int{
		"sessions": len(m.Rounds),
		"ingested": m.ingestedN,
	}
}

func (m *SessionManager) EndSession(sessID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.Forget(sessID)
	delete(m.Rounds, sessID)
	return n
}
