package main

// OpenAI-compatible HTTP API.
// POST /v1/chat/completions - retrieves packed context for the last user msg,
// injects as a system preamble, then either forwards to UPSTREAM_BASE_URL
// (any OpenAI-compatible model server: vLLM/Ollama/OpenAI) or answers locally
// (extraction mode) when no upstream is configured.
// GET /health - liveness.
// X-Context-* headers prove exactly what was injected and under what budget.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	SessionID   string        `json:"session_id,omitempty"`
	MaxContext  int           `json:"max_context_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []ChatChoice   `json:"choices"`
	Usage   map[string]int `json:"usage"`
}

type API struct {
	storePath string
	store     *Store
	sessions  *SessionManager
	upstream  string
	upKey     string
	client    *http.Client
}

func NewAPI(s *Store) *API {
	ap := &API{
		storePath: os.Getenv("STORE_PATH"),
		store:     s,
		sessions:  NewSessionManager(s),
		upstream:  os.Getenv("UPSTREAM_BASE_URL"),
		upKey:     os.Getenv("UPSTREAM_API_KEY"),
		client:    &http.Client{Timeout: 120 * time.Second},
	}
	return ap
}

func lastUserMsg(msgs []ChatMessage) (int, string) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return i, msgs[i].Content
		}
	}
	return -1, ""
}

func (ap *API) buildContext(sessionID, query string, budget int) (string, []string, int) {
	dup := os.Getenv("SANDWICH") != "0"
	return ap.AssembleForModel(sessionID, query, budget, dup)
}

func (ap *API) buildContextLegacy(sessionID, query string, budget int) (string, []string, int) {
	if budget <= 0 {
		budget = 1024
	}
	hits := ap.store.Search(query, budget, 8)
	var parts []string
	total := 0
	srcs := []string{}
	for _, h := range hits {
		if total+h.U.Ntok > budget {
			break
		}
		parts = append(parts, fmt.Sprintf("- [%s %s] %s", h.U.Src, h.U.ID, h.U.Text))
		total += h.U.Ntok
		srcs = append(srcs, h.U.Src)
	}
	if sessionID != "" {
		packed := ap.sessions.PackedContext(sessionID, query)
		for _, p := range packed.Parts {
			if total+estimateTokens(p) <= budget {
				parts = append(parts, "[MEMORY] "+p)
				total += estimateTokens(p)
			}
		}
	}
	return strings.Join(parts, "\n"), srcs, total
}

func (ap *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	idx, userQ := lastUserMsg(req.Messages)
	if idx < 0 || userQ == "" {
		http.Error(w, "no user message", http.StatusBadRequest)
		return
	}

	start := time.Now()
	ctx, srcs, ctxTok := ap.buildContext(req.SessionID, userQ, req.MaxContext)
	injectionMs := float64(time.Since(start).Microseconds()) / 1000

	if req.SessionID != "" {
		ap.sessions.AddTurn(req.SessionID, "user", userQ)
	}

	outMsgs := make([]ChatMessage, 0, len(req.Messages)+1)
	if ctx != "" {
		outMsgs = append(outMsgs, ChatMessage{Role: "system", Content: "Helpful assistant. Use ONLY the following retrieved context; cite [source ids].\n<context>\n" + ctx + "\n</context>"})
	}
	for i, m := range req.Messages {
		if i == idx && ap.upstream == "" {
			continue
		}
		outMsgs = append(outMsgs, m)
	}

	model := req.Model
	if model == "" || model == "auto" {
		model, _ = RouteWithHint(userQ, "research/summarize")
	}

	var resp ChatResponse
	var upstreamBytes []byte
	hdr := w.Header()
	if ap.upstream != "" {
		forward := map[string]interface{}{
			"model":       model,
			"messages":    outMsgs,
			"temperature": req.Temperature,
		}
		fb, _ := json.Marshal(forward)
		upReq, _ := http.NewRequest(http.MethodPost, strings.TrimRight(ap.upstream, "/")+"/chat/completions", bytes.NewReader(fb))
		upReq.Header.Set("Content-Type", "application/json")
		if ap.upKey != "" {
			upReq.Header.Set("Authorization", "Bearer "+ap.upKey)
		}
		upResp, err := ap.client.Do(upReq)
		if err != nil {
			http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer upResp.Body.Close()
		if upResp.StatusCode != 200 {
			eb, _ := io.ReadAll(upResp.Body)
			http.Error(w, "upstream status "+upResp.Status+": "+string(eb), upResp.StatusCode)
			return
		}
		upstreamBytes, _ = io.ReadAll(upResp.Body)
	} else {
		hits := ap.store.Search(userQ, req.MaxContextIf(1024), 8)
		var ans strings.Builder
		ans.WriteString("answer (extraction mode - no upstream configured): ")
		if len(hits) > 0 {
			ans.WriteString(hits[0].U.Text)
		} else {
			ans.WriteString("no relevant context found")
		}
		resp = ChatResponse{
			ID:      "chatcmpl-aicon-" + fmt.Sprint(time.Now().UnixNano()),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []ChatChoice{{Index: 0, Message: ChatMessage{Role: "assistant", Content: ans.String()}, FinishReason: "stop"}},
			Usage:   map[string]int{"prompt_tokens": ctxTok, "completion_tokens": estimateTokens(ans.String()), "total_tokens": ctxTok + estimateTokens(ans.String())},
		}
	}
	hdr.Set("X-Context-Sources", strings.Join(srcs, ","))
	hdr.Set("X-Context-Tokens", fmt.Sprint(ctxTok))
	hdr.Set("X-Context-Injection-Ms", fmt.Sprintf("%.2f", injectionMs))
	hdr.Set("X-Context-TopK", fmt.Sprint(len(srcs)))
	if req.SessionID != "" {
		hdr.Set("X-Session", req.SessionID)
	}
	if req.SessionID != "" && userQ != "" {
		ap.sessions.AddTurn(req.SessionID, "assistant", "answered using: "+strings.Join(srcs, ","))
	}
	hdr.Set("Content-Type", "application/json")
	if upstreamBytes != nil {
		w.Write(upstreamBytes)
	} else if len(resp.Choices) > 0 {
		json.NewEncoder(w).Encode(resp)
	}
}

func (c *ChatRequest) MaxContextIf(def int) int {
	if c.MaxContext > 0 {
		return c.MaxContext
	}
	return def
}

func (ap *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	prov := embeddingProvider()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok", "units": len(ap.store.U), "provider": prov,
		"upstream": ap.upstream != "", "time": time.Now().Format(time.RFC3339),
	})
}

func (ap *API) handleIngest(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Source string `json:"source"`
		Text   string `json:"text"`
		Time   int64  `json:"time"`
		Prov   string `json:"provenance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if a.Time <= 0 {
		a.Time = 2026
	}
	if a.Prov == "" {
		a.Prov = "docs"
	}
	ids := ap.store.Ingest(a.Source, a.Text, a.Prov, a.Time)
	if ap.storePath != "" {
		if err := ap.store.Save(ap.storePath); err != nil {
			fmt.Fprintln(os.Stderr, "save:", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"unit_ids": ids, "count": len(ids)})
}

func RunHTTPServer(addr string) {
	storePath := os.Getenv("STORE_PATH")
	s := LoadStoreOrSeed(storePath)
	ap := NewAPI(s)
	ap.storePath = storePath
	mux := http.NewServeMux()
	mux.HandleFunc("/health", ap.handleHealth)
	mux.HandleFunc("/v1/chat/completions", metricsWrap(ap.handleChat))
	mux.HandleFunc("/ingest", metricsWrapIngest(ap.handleIngest))
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.Write([]byte(renderMetrics(metrics.snapshot())))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"service": "aicon-mini", "version": "1.1.0",
				"endpoints": "/health /v1/chat/completions /ingest",
			})
			return
		}
		http.NotFound(w, r)
	})
	fmt.Fprintf(os.Stderr, "aicon-mini API on %s (provider=%s upstream=%v)\n", addr, embeddingProvider(), ap.upstream != "")
	if storePath != "" {
		if err := s.Save(storePath); err != nil {
			fmt.Fprintln(os.Stderr, "save:", err)
		}
		go func() {
			t := time.Tick(30 * time.Second)
			for range t {
				if err := s.Save(storePath); err != nil {
					fmt.Fprintln(os.Stderr, "save:", err)
				}
			}
		}()
	}
	final := http.Handler(mux)
	if tokEnv := os.Getenv("TOKEN"); tokEnv != "" {
		final = authWrap(mux, true, tokEnv)
	}
	if err := http.ListenAndServe(addr, final); err != nil {
		fmt.Fprintln(os.Stderr, "server:", err)
		os.Exit(1)
	}
}
