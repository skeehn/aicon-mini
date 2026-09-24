package main

// CompressLoop: bounded, deterministic context compaction for an assembled
// candidate set (used by MCP/API when injected context exceeds budget).
// Longest-relevant-first eviction -> extractive summarize -> re-index as Level-1 unit.

import (
	"fmt"
	"strings"
)

type Candidate struct {
	ID    string
	Src   string
	Text  string
	Tok   int
	Vec   []float32
	Score float64
}

type CompressResult struct {
	Parts          []string
	Tokens         int
	Evicted        []string
	SumUnitID      string
	WritebackToken int
}

// CompressLoop packs candidates into budget. Order: by score desc. Eviction:
// among lowest-relevance-per-token items, drop the longest first. Evicted
// content is merged into one extractive summary and (optionally) ingested into
// the store as a Level-1 unit so nothing is truly lost.
func CompressLoop(s *Store, query string, cands []Candidate, budget int, writeBack bool) CompressResult {
	for i := range cands {
		if cands[i].Vec == nil {
			cands[i].Vec = vecOf(tok(cands[i].Text))
		}
		if cands[i].Tok == 0 {
			cands[i].Tok = estimateTokens(cands[i].Text)
		}
	}
	// sort score desc
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].Score > cands[i].Score {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	used := 0
	var keep []Candidate
	var evict []Candidate
	for _, c := range cands {
		if used+c.Tok <= budget {
			keep = append(keep, c)
			used += c.Tok
		} else {
			evict = append(evict, c)
		}
	}
	// if still over (a single huge candidate), shrink from end
	for keep != nil && used > budget {
		last := keep[len(keep)-1]
		if sum := extractiveWithin(last.Text, budget-(used-last.Tok)); sum != "" {
			cut := Candidate{ID: last.ID, Src: last.Src, Text: sum, Tok: estimateTokens(sum), Vec: last.Vec, Score: last.Score}
			used = used - last.Tok + cut.Tok
			keep[len(keep)-1] = cut
			break
		}
		used -= last.Tok
		evict = append(evict, last)
		keep = keep[:len(keep)-1]
	}
	// write-back summary for evicted
	res := CompressResult{Tokens: used}
	for _, k := range keep {
		res.Parts = append(res.Parts, "- ["+k.Src+"] "+k.Text)
	}
	if len(evict) > 0 && len(keep) > 0 {
		var texts []string
		for _, e := range evict {
			texts = append(texts, "[["+e.Src+"]] "+e.Text)
			res.Evicted = append(res.Evicted, e.Src)
		}
		summary := summarizeExtractive(strings.Join(texts, " "))
		if writeBack && s != nil {
			ids := s.Ingest("compressed", summary, "summary", int64(nowYear()))
			if len(ids) > 0 {
				res.SumUnitID = ids[0]
				s.AdjLevel(ids[0], 1)
			}
		}
		res.Parts = append(res.Parts, "[WRITE-BACK SUMMARY] "+summary)
		res.WritebackToken = estimateTokens(summary)
	}
	return res
}

func nowYear() int64 { return int64(2026) }

func extractiveWithin(text string, maxTok int) string {
	if maxTok <= 0 {
		return ""
	}
	sents := splitSentences(text)
	var out []string
	n := 0
	for _, sn := range sents {
		t := estimateTokens(sn)
		if maxTok > 0 {
			if n+t > maxTok {
				break
			}
			out = append(out, sn)
			n += t
		} else {
			break
		}
	}
	_ = fmt.Sprint
	return strings.Join(out, " ")
}
