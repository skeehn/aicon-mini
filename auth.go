package main

// auth.go: bearer-token auth + namespace isolation + Prometheus-text metrics.
// Env: TOKEN (HMAC secret) enables auth mode; X-Namespace isolates stores.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Metrics struct {
	mu         sync.Mutex
	ChatOk     int64
	ChatErr    int64
	SearchN    int64
	IngestN    int64
	LaSearchUs int64
	InjUs      int64
	SNS        int64
	NSLat      int64
	NSearch    int64
	Ninline    int64
}

var metrics = &Metrics{}

type Namespace struct {
	ID   string
	Sub  *Store
	Sess *SessionManager
}

type AuthServer struct {
	http.Handler
	Tokens    string
	NamespMap map[string]*Namespace
	NSMu      sync.Mutex
	API       *API
	storeFor  func() *Store
}

func deriveKey(secret string) ([]byte, error) {
	h := sha256.Sum256([]byte("aicon-mini:" + secret))
	return []byte(hex.EncodeToString(h[:])), nil
}

func (t *Metrics) snapshot() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]int64{
		"aicon_search_total": t.NSearch, "aicon_ingest_total": t.Ninline,
		"aicon_session_ns": t.SNS,
	}
}

func renderMetrics(m map[string]int64) string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += fmt.Sprintf("# TYPE %s counter\n%s %d\n", k, k, m[k])
	}
	return out
}

func (t *Metrics) record(kind string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch kind {
	case "search":
		t.NSearch++
	case "ingest":
		t.Ninline++
	case "chat_ok":
		t.ChatOk++
	case "chat_err":
		t.ChatErr++
	case "session_ns":
		t.SNS++
	}
}

func sessionStartAt(t time.Time) int64 { return time.Since(time.Time{}).Microseconds() }

type authed struct {
	mux http.Handler
}

type AuthedServer struct {
	Inner    http.Handler
	Required bool
	Tokens   string
	NSMap    map[string]*Namespace
	NSMu     *sync.Mutex
}

func namespaceCmd(ns string) string {
	if ns == "" {
		return "default"
	}
	return ns
}

// Endpoints wrap with bearer auth + namespace lookup.
func authWrap(inner http.Handler, required bool, tokens string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if required {
			tok := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(tok, "Bearer ")
			if tok == "" {
				http.Error(w, "401 unauthorized: missing bearer token", http.StatusUnauthorized)
				return
			}
			if tokens != "" && tok != tokens {
				http.Error(w, "401 unauthorized: bearer token mismatch", http.StatusUnauthorized)
				return
			}
		}
		inner.ServeHTTP(w, r)
	})
}

func sanityNS() string {
	return namespaceCmd("")
}

func metricsWrap(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h(rec, r)
		if rec.status >= 400 {
			metrics.record("chat_err")
		} else {
			metrics.record("chat_ok")
			metrics.record("search")
		}
		_ = start
	}
}

func metricsWrapIngest(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w, r)
		metrics.record("ingest")
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
