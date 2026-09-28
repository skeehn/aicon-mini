package main

// truth.go: Cilow conflict-op ported to aicon-mini's model.
// resolveWrite: source-scoped exclusive semantics. If same Src already has an
// active unit with DIFFERENT content hash, we never auto-overwrite: mark NEW
// unit Conflicted, leave old active. Conflicted units never enter the answer
// pack or the retrieval ranking (like Cilow's Conflict flag, read-path gated).

import (
	"sync"
)

type ConflictRegistry struct {
	mu      sync.Mutex
	Active  map[string]bool   // unitId -> conflicted
	Pairs   [][2]string       // (existing, incoming) pairs
	Reasons map[string]string // unitId -> reason
}

func NewConflictRegistry() *ConflictRegistry {
	return &ConflictRegistry{
		Active:  map[string]bool{},
		Pairs:   [][2]string{},
		Reasons: map[string]string{},
	}
}

// SameSrcDifferentContent: hash text (FNV-equivalent via sha256 short). Deterministic.
func sameSrcDifferentContent(existing *Unit, incoming string) bool {
	if existing.Text == incoming {
		return false
	}
	h1 := sha256Short(existing.Text)
	h2 := sha256Short(incoming)
	return h1 != h2
}

func sha256Short(s string) string {
	h := fnvShort(s)
	return h
}

// ResolveWrite: user-approved Cilow truth algebra. Returns (conflicted, existing).
// Same-src-different-content, same provenance channel 'policy'-like exclusivity
// via provIsExclusive = prov in {policy, db, compliance}. For exclusive channels
// only, to avoid conflict-flagging benign mirror duplicates in bench corpora.
func (s *Store) ResolveWrite(src, text, prov string) bool {
	if prov != "policy" && prov != "db" && prov != "compliance" {
		return false
	}
	for _, u := range s.U {
		if u.Src != src || s.Tomb[u.ID] || s.Conflicts.Active[u.ID] {
			continue
		}
		if sameSrcDifferentContent(u, text) {
			// conflict, never auto-overwrite
			return true
		}
	}
	return false
}

// ResolveWriteMark is called from ingestLocked; when true, incoming unit gets
// Conflicted flag and is excluded from retrieval rank but kept inspectable.
func (s *Store) ResolveWriteMark(src, text, prov, newID string) bool {
	conf := s.ResolveWrite(src, text, prov)
	if !conf {
		return false
	}
	s.mu.Lock() // external call needs the same lock
	defer s.mu.Unlock()
	if s.Conflicts == nil {
		s.Conflicts = NewConflictRegistry()
	}
	s.Conflicts.Active[newID] = true
	s.Conflicts.Reasons[newID] = "exclusive-channel content mismatch"
	for _, u := range s.U {
		if u.Src == src && u.ID != newID {
			s.Conflicts.Pairs = append(s.Conflicts.Pairs, [2]string{u.ID, newID})
			break
		}
	}
	return true
}

func fnvShort(s string) string {
	hash := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= 1099511628211
	}
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(hash >> (i * 8))
	}
	return string(b)
}
