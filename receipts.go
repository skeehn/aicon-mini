package main

// receipts.go: assembly receipt - full provenance per query.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

type Receipt struct {
	ID          string   `json:"receipt_id"`
	Query       string   `json:"query"`
	Units       int      `json:"units"`
	UnitIDs     []string `json:"unit_ids"`
	Sources     []string `json:"sources"`
	Tokens      int      `json:"tokens"`
	Intent      string   `json:"intent"`
	ConformalNC float64  `json:"conformal_nc"`
	Abstain     bool     `json:"abstain"`
	PprPull     int      `json:"ppr_added"`
	Hops        int      `json:"chain_hops"`
	Why         []string `json:"why_trace"`
	Hash        string   `json:"hash"`
}

type ReceiptStore struct {
	mu sync.Mutex
	m  map[string]*Receipt
}

var receipts = &ReceiptStore{m: map[string]*Receipt{}}

func (r *Receipt) fillHash() {
	b, _ := json.Marshal([]interface{}{r.Query, r.UnitIDs, r.Sources, r.Tokens, r.Intent, r.ConformalNC, r.Why})
	h := sha256.Sum256(b)
	r.Hash = hex.EncodeToString(h[:])[:16]
}

func (rs *ReceiptStore) Put(r *Receipt) {
	r.fillHash()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.m[r.ID] = r
}

func (rs *ReceiptStore) Get(id string) (*Receipt, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r, ok := rs.m[id]
	return r, ok
}

func newReceiptID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (r *Receipt) Short() string {
	return r.ID + " h=" + r.Hash + " units=" + strconv.Itoa(r.Units) +
		" tok=" + strconv.Itoa(r.Tokens) + " intent=" + r.Intent +
		" nc=" + strconv.FormatFloat(r.ConformalNC, 'f', 2, 64) +
		" ppr=" + strconv.Itoa(r.PprPull) + " hops=" + strconv.Itoa(r.Hops)
}

func intentName(i Intent) string {
	return map[Intent]string{
		IntentFactual:     "factual",
		IntentTemporal:    "temporal",
		IntentPreference:  "preference",
		IntentAggregation: "aggregation",
	}[i]
}

func receiptsList() []*Receipt {
	receipts.mu.Lock()
	defer receipts.mu.Unlock()
	var out []*Receipt
	for _, r := range receipts.m {
		out = append(out, r)
	}
	return out
}
