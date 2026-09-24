package main

// MCP (Model Context Protocol) stdio server.
// Tools: context_search, context_ingest, context_inspect, context_compress,
// context_session_report. JSON-RPC 2.0 over stdin/stdout, newline-delimited.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"params"`
}

type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type MCPPager struct {
	store     *Store
	sessions  *SessionManager
	budgetCap int
}

func NewMCPPager(s *Store) *MCPPager {
	return &MCPPager{store: s, sessions: NewSessionManager(s), budgetCap: 2048}
}

var mcpTools = `{"tools":[{"name":"context_search","description":"Hybrid BM25+dense+graph search over the knowledge store. Returns packed hits within a token budget with provenance.","inputSchema":{"type":"object","properties":{"query":{"type":"string"},"budget":{"type":"integer","default":120},"k":{"type":"integer","default":5}},"required":["query"]}},{"name":"context_ingest","description":"Ingest a document (auto-chunked, CAS contents-addressed; re-ingest is a no-op). Returns unit ids.","inputSchema":{"type":"object","properties":{"source":{"type":"string"},"text":{"type":"string"},"time":{"type":"integer","default":2026},"provenance":{"type":"string","default":"docs"}},"required":["source","text"]}},{"name":"context_inspect","description":"Inspect a unit id: full text plus 1-hop neighbors.","inputSchema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}},{"name":"context_compress","description":"Given candidate texts, pack them under a token budget with longest-relevant-first eviction and extractive summary write-back.","inputSchema":{"type":"object","properties":{"query":{"type":"string"},"texts":{"type":"array","items":{"type":"string"}},"budget":{"type":"integer","default":512}},"required":["query","texts"]}},{"name":"context_session_report","description":"Session memory stats: turns kept, summary tokens, compacted count, packed context for a query.","inputSchema":{"type":"object","properties":{"session_id":{"type":"string"},"role":{"type":"string"},"content":{"type":"string"},"query":{"type":"string"}},"required":["session_id"]}}]}`

func (m *MCPPager) handleTool(name string, args json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "context_search":
		var a struct {
			Query  string `json:"query"`
			Budget int    `json:"budget"`
			K      int    `json:"k"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Budget <= 0 {
			a.Budget = 120
		}
		if a.K <= 0 {
			a.K = 5
		}
		hits := m.store.Search(a.Query, a.Budget, a.K)
		type hitJson struct {
			ID    string  `json:"id"`
			Src   string  `json:"source"`
			Text  string  `json:"text"`
			Tok   int     `json:"tokens"`
			Level int     `json:"level"`
			Score float64 `json:"score"`
			Why   string  `json:"why"`
		}
		var out []hitJson
		total := 0
		for _, h := range hits {
			out = append(out, hitJson{h.U.ID, h.U.Src, h.U.Text, h.U.Ntok, h.U.Level, h.Score, h.Why})
			total += h.U.Ntok
		}
		return json.Marshal(map[string]interface{}{
			"hits": out, "tokens": total, "budget_used_pct": float64(total) / float64(a.Budget) * 100,
		})
	case "context_ingest":
		var a struct {
			Source string `json:"source"`
			Text   string `json:"text"`
			Time   int64  `json:"time"`
			Prov   string `json:"provenance"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Time <= 0 {
			a.Time = 2026
		}
		if a.Prov == "" {
			a.Prov = "docs"
		}
		ids := m.store.Ingest(a.Source, a.Text, a.Prov, a.Time)
		return json.Marshal(map[string]interface{}{"unit_ids": ids, "count": len(ids)})
	case "context_inspect":
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		units := m.store.Inspect(a.ID, 1)
		if len(units) == 0 {
			return json.Marshal(map[string]interface{}{"error": "not found"})
		}
		type uj struct {
			ID    string `json:"id"`
			Src   string `json:"source"`
			Text  string `json:"text"`
			Level int    `json:"level"`
			Tomb  bool   `json:"tombstoned"`
			SupBy string `json:"superseded_by,omitempty"`
		}
		var out []uj
		for _, u := range units {
			out = append(out, uj{u.ID, u.Src, u.Text, u.Level, m.store.Tomb[u.ID], m.store.Sup[u.ID]})
		}
		return json.Marshal(map[string]interface{}{"units": out})
	case "context_compress":
		var a struct {
			Query  string   `json:"query"`
			Texts  []string `json:"texts"`
			Budget int      `json:"budget"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Budget <= 0 {
			a.Budget = 512
		}
		var cands []Candidate
		for i, t := range a.Texts {
			cands = append(cands, Candidate{
				ID:   fmt.Sprintf("cand-%d", i),
				Src:  fmt.Sprintf("in-%d", i),
				Text: t,
				Tok:  estimateTokens(t),
				Vec:  vecOf(tok(t)),
			})
		}
		res := CompressLoop(m.store, a.Query, cands, a.Budget, true)
		return json.Marshal(map[string]interface{}{
			"parts": res.Parts, "tokens": res.Tokens, "evicted_sources": res.Evicted, "summary_unit": res.SumUnitID,
		})
	case "context_session_report":
		var a struct {
			SessionID string `json:"session_id"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			Query     string `json:"query"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Role != "" && a.Content != "" {
			m.sessions.AddTurn(a.SessionID, a.Role, a.Content)
		}
		if a.Query == "" {
			a.Query = a.Content
		}
		packed := m.sessions.PackedContext(a.SessionID, a.Query)
		stats := m.sessions.Stats()
		return json.Marshal(map[string]interface{}{
			"packed_parts": packed.Parts, "tokens": packed.Tokens,
			"dropped": packed.Dropped, "stats": stats,
		})
	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}

func (m *MCPPager) Serve(reader io.Reader, writer io.Writer) error {
	sc := bufio.NewScanner(reader)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := MCPResponse{JSONRPC: "2.0"}
			bad, _ := json.Marshal(resp)
			fmt.Fprintln(writer, string(bad))
			continue
		}
		switch req.Method {
		case "initialize":
			res, _ := json.Marshal(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]bool{"listChanged": false}},
				"serverInfo":      map[string]string{"name": "aicon-mini", "version": "1.1.0"},
			})
			resp := MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
			b, _ := json.Marshal(resp)
			fmt.Fprintln(writer, string(b))
		case "tools/list":
			resp := MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(mcpTools)}
			b, _ := json.Marshal(resp)
			fmt.Fprintln(writer, string(b))
		case "tools/call":
			var res json.RawMessage
			var errMsg string
			var err error
			if strings.TrimSpace(string(req.Params.Name)) == "" {
				errMsg = "missing tool name"
			} else {
				res, err = m.handleTool(req.Params.Name, req.Params.Arguments)
				if err != nil {
					errMsg = err.Error()
				}
			}
			if errMsg != "" {
				e := struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{-32603, errMsg}
				resp := MCPResponse{JSONRPC: "2.0", ID: req.ID}
				b0, _ := json.Marshal(resp)
				var m2 map[string]interface{}
				json.Unmarshal(b0, &m2)
				m2["error"] = e
				b, _ := json.Marshal(m2)
				fmt.Fprintln(writer, string(b))
				continue
			}
			full, _ := json.Marshal(map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(res)}}})
			resp := MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: full}
			b, _ := json.Marshal(resp)
			fmt.Fprintln(writer, string(b))
		default:
			resp := MCPResponse{JSONRPC: "2.0", ID: req.ID}
			b0, _ := json.Marshal(resp)
			var m2 map[string]interface{}
			json.Unmarshal(b0, &m2)
			m2["error"] = map[string]interface{}{"code": -32601, "message": "method not found: " + req.Method}
			b, _ := json.Marshal(m2)
			fmt.Fprintln(writer, string(b))
		}
	}
	return sc.Err()
}

func RunMCPServer() {
	s := NewStore()
	seedInto(s)
	m := NewMCPPager(s)
	if err := m.Serve(os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
