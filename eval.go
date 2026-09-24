package main

// TASK-SUITE eval: 12 tasks over a generated noisy corpus. Deterministic in
// hash mode (CI-safe, no keys). Metrics: Recall@5/@10, MRR, tokens.
// Run: go run . taskeval - writes eval_report.json, exit 1 on regression.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

func rankOnly2(s *Store, q, mode string, budget, k int) []Hit { return rankOnly(s, q, mode, budget, k) }

type noiseDoc struct{ src, text string }

func noiseCorpus() []noiseDoc {
	topics := []string{
		"The onboarding pipeline validates email syntax before provisioning SSO credentials.",
		"Feature flags expire after 90 days of inactivity and are garbage-collected nightly.",
		"The design system uses a 8pt spacing grid with 4pt micro-adjustments for dense tables.",
		"Postgres replicas lag behind primary by 40ms p50 under steady read load.",
		"Error budget policy freezes feature deploys when the error budget is exhausted two consecutive weeks.",
		"The style guide bans em-dash in UI copy and requires sentence case for buttons.",
		"Nightly backups are encrypted with per-tenant KMS keys and replicated cross-region.",
		"A/B tests must power a full week (168h) to cover weekday and weekend traffic patterns.",
		"The compiler cache keys on toolchain hash plus dependency lockfile digest.",
		"Rate limit headers expose remaining quota as X-RateRemaining seconds until reset.",
		"Compliance sign-off requires SOC2 evidence exports refreshed every 90 days.",
		"Internal search indexing runs every 15 minutes with incremental crawl budgets.",
		"The data retention policy keeps audit logs for 90 days then archives to cold storage with AES-256.",
		"Load balancer health checks use/books 200 responses with 2s timeout and 3 retries across availability zones.",
		"The migration workflow uses double-writes with read verification for one release cycle.",
		"Redis persistence is RDB snapshots every 5 minutes and AOF fsync every second.",
		"Grafana dashboards are provisioned from IaC with per-team access scopes.",
		"Single sign-out propagates session revocation to all registered devices within 5 seconds.",
		"The ML feature store expires training snapshots after 180 days to limit data leakage.",
		"Queues use exponential backoff with jitter: base 500ms, factor 2, cap 30s, max 5 attempts.",
	}
	noise10 := []string{
		"Grain silos imagery shows 2% moisture variance across the eastern field this quarter.",
		"The orchestra tuned instruments for 12 minutes before the conductor arrived.",
		"Septic inspection reports are filed with the county clerk every three years.",
		"Vintage bicycle gearing ratios favor 52/14 for downhill sprints.",
		"Ancient Mesopotamian tablets record barley rations in shekel units.",
	}
	docs := []noiseDoc{}
	for i, t := range topics {
		docs = append(docs, noiseDoc{fmt.Sprintf("topic-%02d", i/2+1), t})
	}
	for i, n := range noise10 {
		docs = append(docs, noiseDoc{fmt.Sprintf("noise-%02d", i+1), n})
	}

	docs = append(docs, noiseDoc{"temporal-v1", "Secret rotation inspection: current token expiry is 60 days. Signed by Dana 2023. Policy v1 archived."})
	docs = append(docs, noiseDoc{"temporal-v2", "Secret rotation inspection: current token expiry is 30 days. Signed by Elena 2025. Policy v2 supersedes v1.4."})
	docs = append(docs, noiseDoc{"old-2020", "Expired exposure: token expiry 365 days per the 2020 draft. Superseded. Ignored."})
	docs = append(docs, noiseDoc{"phone-a", "Support hotline dials 555-0100 during business hours as of 2023."})
	docs = append(docs, noiseDoc{"phone-b", "Support hotline dialed 555-0199 until 2022; older listings may still reference it."})
	docs = append(docs, noiseDoc{"db-hops-1", "Service mesh tasks run on the Copper platform which stores config in Homebase."})
	docs = append(docs, noiseDoc{"Homebase-hop-2", "Homebase config supports ARIA feature flags and TLS 1.3 ciphersuites only."})
	docs = append(docs, noiseDoc{"quota-v1", "Batch job quota is 8 concurrent workers."})
	docs = append(docs, noiseDoc{"quota-v2", "Batch job quota raised to 16 concurrent workers in 2024. Supersedes 8."})
	docs = append(docs, noiseDoc{"paraphrase-1", "Customers may return unchanged goods within thirty days of purchase against a receipt."})
	docs = append(docs, noiseDoc{"paraphrase-2", "Refunds are processed inside four weeks once verification completes."})
	docs = append(docs, noiseDoc{"doc-heavy-a", "ML experiment tracker writes 40 metrics per run; retention 60 days; export requires read scope."})
	docs = append(docs, noiseDoc{"doc-heavy-b", "MCP knowledge base contents: dashboards, metrics, runbook snippets, quota references and API scopes."})
	docs = append(docs, noiseDoc{"doc-heavy-c", "Approval chain requires two managers plus compliance only for Tier-1 streets."})
	return docs
}

type Task struct {
	name string
	q    string
	gold []string
	kind string
}

func taskSuite() []Task {
	n := noiseCorpus()
	find := func(src string) string {
		for _, d := range n {
			if d.src == src {
				return d.text
			}
		}
		return ""
	}
	_ = find
	return []Task{
		{"single-hop-fact", "Which platform stores its config in Homebase?", []string{"db-hops-1"}, "fact"},
		{"single-hop-detail", "How many metrics does experiment tracking write per run?", []string{"doc-heavy-a"}, "fact"},
		{"single-hop-approval", "Who approves Tier-1 change requests?", []string{"doc-heavy-c"}, "fact"},
		{"temporal-current", "Current token expiry after 2024?", []string{"temporal-v2"}, "temporal"},
		{"temporal-hotline", "What is the support hotline after 2023?", []string{"phone-a"}, "temporal"},
		{"tombstone-hotline", "Support hotline 555-0199 is referenced where and when valid?", []string{"phone-b"}, "temporal"},
		{"tombstone-quota", "Prior concurrent worker quota", []string{"quota-v1"}, "tomb"},
		{"temporal-quota", "Current concurrent worker quota raise", []string{"quota-v2"}, "temporal"},
		{"paraphrase-returns", "Can customers send bought items back?", []string{"paraphrase-1"}, "paraphrase"},
		{"paraphrase-refund", "When will money arrive after verification?", []string{"paraphrase-2"}, "paraphrase"},
		{"multi-hop-aria", "Which config platform supports ARIA flags?", []string{"Homebase-hop-2"}, "multihop"},
		{"multi-doc-tls", "Does Homebase allow TLS 1.3?", []string{"Homebase-hop-2"}, "multihop"},
	}
}

func mrr(gold map[string]bool, hits []Hit, at int) float64 {
	for i := 0; i < len(hits) && i < at; i++ {
		if gold[hits[i].U.Src] {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func ndcgAt5(gold map[string]bool, hits []Hit, at int) float64 {
	dcg := 0.0
	for i := 0; i < len(hits) && i < at; i++ {
		rel := 0.0
		if gold[hits[i].U.Src] {
			rel = 1
		}
		dcg += rel / math.Log2(float64(i+2))
	}
	idcg := 0.0
	for i := 0; i < at; i++ {
		idcg += 1 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func recallKOft(gold map[string]bool, hits []Hit, at int) float64 {
	got := 0
	total := len(gold)
	for i := 0; i < len(hits) && i < at; i++ {
		if gold[hits[i].U.Src] {
			got++
		}
	}
	if total == 0 {
		return 1
	}
	return float64(got) / float64(total)
}

func runTaskEval() int {
	s := NewStore()
	for _, d := range noiseCorpus() {
		s.Ingest(d.src, d.text, "docs", 2026)
	}
	tasks := taskSuite()
	budget, k := 160, 10
	var hR5, hR10, hM, hN, bR5, bR10, bM, hT, bT float64
	type perTask struct {
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		HybridR5   float64 `json:"hybrid_r5"`
		HybridR10  float64 `json:"hybrid_r10"`
		HybridMRR  float64 `json:"hybrid_mrr"`
		HybridNDCG float64 `json:"hybrid_ndcg5"`
		BM25R5     float64 `json:"bm25_r5"`
		BM25R10    float64 `json:"bm25_r10"`
		BM25MRR    float64 `json:"bm25_mrr"`
		HyTok      int     `json:"hybrid_tokens"`
		BMTok      int     `json:"bm25_tokens"`
	}
	var per []perTask
	for _, t := range tasks {
		g := map[string]bool{}
		for _, x := range t.gold {
			g[x] = true
		}
		h := s.Search(t.q, budget, k)
		b := rankOnly(s, t.q, "bm25", budget, k)
		d := rankOnly(s, t.q, "dense", budget, k)
		hR5 += recallKOft(g, h, 5)
		hR10 += recallKOft(g, h, 10)
		hM += mrr(g, h, k)
		hN += ndcgAt5(g, h, 5)
		bR5 += recallKOft(g, b, 5)
		bR10 += recallKOft(g, b, 10)
		bM += mrr(g, b, 10)
		hT += float64(toks(h))
		bT += float64(toks(b))
		per = append(per, perTask{
			Name: t.name, Kind: t.kind,
			HybridR5: round3(recallKOft(g, h, 5)), HybridR10: round3(recallKOft(g, h, 10)),
			HybridMRR: round3(mrr(g, h, 10)), HybridNDCG: round3(ndcgAt5(g, h, 5)),
			BM25R5: round3(recallKOft(g, b, 5)), BM25R10: round3(recallKOft(g, b, 10)), BM25MRR: round3(mrr(g, b, 10)),
			HyTok: toks(h), BMTok: toks(b),
		})
		_ = d
	}
	n := float64(len(tasks))
	hR5, hR10, hM, hN = hR5/n, hR10/n, hM/n, hN/n
	bR5, bR10, bM = bR5/n, bR10/n, bM/n
	hT, bT = hT/n, bT/n
	fmt.Printf("TASK-SUITE 12 docs noisy corpus\n")
	for _, p := range per {
		fmt.Printf("  %-22s %s  hybrid R@5=%.2f R@10=%.2f MRR=%.2f tok=%d | bm25 R@5=%.2f R@10=%.2f tok=%d\n", p.Name, p.Kind, p.HybridR5, p.HybridR10, p.HybridMRR, p.HyTok, p.BM25R5, p.BM25R10, p.BMTok)
	}
	fmt.Printf("\nAGG hybrid R@5=%.3f R@10=%.3f MRR=%.3f NDCG@5=%.3f tok=%.0f | bm25 R@5=%.3f R@10=%.3f MRR=%.3f tok=%.0f\n",
		hR5, hR10, hM, hN, hT, bR5, bR10, bM, bT)

	pass := hR10 >= 0.95 && hM >= 0.75 && hM >= bM-0.15 && hT <= bT*1.05
	if pass {
		fmt.Printf("PASS: R@10>=0.95, MRR>=0.75 (within 0.15 of BM25), tokens within 5 pct\n")
	} else {
		fmt.Printf("FAIL: targets missed\n")
	}

	rep := map[string]interface{}{
		"n_tasks": len(tasks), "budget": budget, "k": k,
		"hybrid": map[string]float64{"recall@5": hR5, "recall@10": hR10, "mrr": hM, "ndcg@5": hN, "tokens_avg": hT},
		"bm25":   map[string]float64{"recall@5": bR5, "recall@10": bR10, "mrr": bM, "tokens_avg": bT},
		"pass":   pass,
	}
	out, _ := json.MarshalIndent(rep, "", "  ")
	os.WriteFile("eval_report.json", append(out, '\n'), 0644)
	if pass {
		return 0
	}
	return 1
}

func round3(f float64) float64 {
	return math.Round(f*1000) / 1000
}
