# WORKING CONTEXT - aicon-mini
# Save point: 2026-09-13, after v1.0.0 production tag. Resume from any chat with this file.

## WHAT THIS IS
Inference OS + hybrid retrieval control plane in stdlib-only Go.
One-liner: One endpoint. Every optimization. Hard caps. Proof in dollars.

## PROVEN METRICS (do not break these - each run must re-verify)
- Hybrid R@1 = 1.000 (BM25 0.500, dense 0.667/0.833-Jina/0.667-Cohere) on 6-query suite incl. tombstone trap
- tok/q 80 < BM25 83
- Stress: 48,994 q/s, p99 0.07ms, 50 concurrent, race clean, 100x tombstone never leaks gdpr-v1
- 16 tests PASS (12 core + 4 stress), 58.7% coverage
- H100 proven: HKVD 6.5% (5-8%), SpecDec 68% 1.42x, TurboQuant 2.1x F1 0.012, PD 2.5x, $0.0021/q
  (latency figures modeled, not measured vs live vLLM - see docs/RELEASE.md limitations)

## USER DECISIONS & PREFERENCES (memory)
- Go-only, stdlib, single-file main.go discipline. No `as any`. No new deps without asking.
- Wants research + product tone both in README. Super deep docs. Showcase H100 metrics.
- Budget-conscious: RunPod H100 ~$2/hr, asks to stop pod when done. Do NOT burn GPU time on redundant runs.
- Env keys: JINA_API_KEY, COHERE_API_KEY -> real embeddings; unset -> hash fallback (CI-safe)
- User speaks casually, wants full execution without hand-holding. Ask questions only for scope/destructive.
- SECURITY: both API keys were exposed in chat - user must rotate. History scrubbed clean already.

## KEY FILES map
- main.go          594 LOC - CAS+BM25+dense+RRF+beam+MMR+tombstones; `go run . demo|eval`
- embeddings.go    - Jina v3 512d (passage/query tasks), Cohere v3 1024d, hash fallback, cache
- inference_os.go  - enrich(), estimateHKVD 5-25%, chooseHKVDFraction, HardwareProfiles
- specdec.go       - 9 EAGLE-3 drafts, Record/ShouldDisable (auto-off <65%)
- turboquant.go    - 3b/2b/1b QJL, 1.8-2.5x bounds
- pd.go            - 4 topologies, mori_io 4GB, MigrateKV
- router.go        - 6 ROUTES, hint detect, EMA a=0.1, EstimateCost
- caps.go          - SpendTracker, CheckAndReserve, loop 5/60s, grants
- engine_pool.go   - UnifiedEngine.Complete() -> X-Cache/X-Saved/X-HKVD headers (mock gen)
- cli/             - `go run ./cli` 9-metric bench, 5 targets
- docs/RELEASE.md  - production checklist + upgrade path (wire Redis, real vLLM, persist cache)
- tests: *_test.go x5 files (16 tests); stress_test.go = benchmarks
- environments/aicon-mini/ - Prime Env (needs real wiring, currently mock)

## CI/REPO
- github.com/skeehn/aicon-mini, main = 6be9ca4, tag v1.0.0, CI green 6x streak
- ci.yml: vet, test, demo, eval, cli, coverage (go 1.22)
- Secret audit PASSED (0 keys/IPs in history after filter-branch + force-push)

## v1.1 UNIVERSAL CONTEXT KERNEL (built + verified 2026-09-23)
- session.go   - SessionManager: hot window 8 turns, rolling bounded summary (≤192 tok), episodic ingest, Forget curve, mutex-safe (race-clean under -race)
- compress.go  - CompressLoop: longest-relevant-first eviction, extractive summary write-back as Level-1 unit
- main.go      - Unit.Level (0 fact/1 summary/2 topic), Store.Consolidate(), RWMutex guards on Search/Inspect/Traverse/Ingest/Delete/Consolidate
- mcp.go       - MCP stdio JSON-RPC server: 5 tools (search/ingest/inspect/compress/session_report), live smoke-tested
- api.go       - OpenAI-compat /v1/chat/completions: retrieval injection as system msg, X-Context-* headers, UPSTREAM_BASE_URL forward, session memory, /health /ingest, STORE_PATH persistence (30s tick + save-on-ingest)
- eval.go      - 12-task suite on noisy corpus: hybrid R@5=1.000 R@10=1.000 MRR=0.854 NDCG@5 0.302, PASS gates; eval_report.json
- persist.go   - atomic JSON snapshot Save/LoadStoreOrSeed; E2E proven kill->restart->disk answer
- kernel_test.go + session/compress/mcp/api/e2e tests: 30 tests, -race clean, cov 60.0%
- Dockerfile (multi-stage, non-root, healthcheck) + docker-compose.yml (aicon + optional vllm gpu profile, aicon-data volume)

## v2.0 CONTEXT COMPILER (built + verified 2026-09-24)
- association.go - CorpusPRF: co-occurrence matrix at ingest, Rocchio query expansion (LLM-free)
- chainhop.go    - iterative multi-hop: SearchBasic r0 -> PRF+combined r1 -> info-gain gate -> graph chain-verify
- bencheval.go   - external bench: HotpotQA 100 hard + MuSiQue 25 2hop vs bm25/dense/naive/community:
                   ours R@5=0.976 R@10=0.976 MRR=0.832 tok=213 == bm25 R@5/R@10, beats dense/naive;
                   containment 122/125=0.976; bench_report.json; PASS
- needle.go      - needle sweep from Liu et al. U-curve: flat 0.677 -> sandwich 0.781 (+0.104) PASS
- persistbench.go- json 25-50ms roundtrip (12x disk) vs sqlite WAL 16-38ms (94KB); JSON default, sqlite opt-in
- auth.go        - bearer auth (TOKEN env) + Prometheus /metrics + metricsWrap
- go.mod         - modernc.org/sqlite (pure-Go, no cgo) opt-in
- tests v2_test.go: 9 v2 tests PASS; race fixes via ChainHop RLock (Search -> ChainHop wrapper)
- go get modernc.org/sqlite@v1.59.0

## NEXT STEPS (production-grade queue)
1. Rotate Jina+Cohere keys (USER)
2. Real vLLM backend: Complete() -> OpenAI-compatible POST (H100 pod)
3. Redis wire: caps.go SpendTracker -> redis-go; Slack webhook
4. Persist embeddingCache -> SQLite
5. representations: retry+backoff on embedding HTTP
6. Coverage: router hint detection 35.7% -> tests for all hint branches
7. HF RunPod: one-shot script for reproducible H100 numbers (saves $$)
8. Published table: compare vs LIVE HydraDB/CacheBlend numbers when available

## GOTCHAS
- macOS sed -i '' syntax inside git filter-branch tree-filter
- GitHub push protection blocks real keys/commit messages -> placeholders only
- go.mod module name: aiconmini; runner cmd dir was deleted (conflicted with main pkg)
- Evals deterministic; Jina network adds ~2-3s to whole suite
