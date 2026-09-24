package main

// Persistence: atomic JSON snapshot of the whole Store (units, tombstones,
// supersede links, adjacency, DF). Stdlib only. Atomic via temp+rename.

import (
	"encoding/json"
	"os"
)

type snapshot struct {
	Version int               `json:"version"`
	U       []*Unit           `json:"units"`
	Adj     map[string][]Rel  `json:"adj"`
	Tomb    map[string]bool   `json:"tomb"`
	Sup     map[string]string `json:"sup"`
}

func (s *Store) Save(path string) error {
	if path == "" {
		path = "store.json"
	}
	snap := snapshot{
		Version: 1,
		U:       s.U,
		Adj:     s.Adj,
		Tomb:    s.Tomb,
		Sup:     s.Sup,
	}
	b, err := json.MarshalIndent(snap, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadStore(path string) (*Store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	s := NewStore()
	s.U = snap.U
	s.Tomb = snap.Tomb
	s.Sup = snap.Sup
	if s.Tomb == nil {
		s.Tomb = map[string]bool{}
	}
	if s.Sup == nil {
		s.Sup = map[string]string{}
	}
	for _, u := range s.U {
		s.ByID[u.ID] = u
	}
	s.Adj = snap.Adj
	if s.Adj == nil {
		s.Adj = map[string][]Rel{}
	}
	s.N = len(s.U)
	tot := 0
	for _, u := range s.U {
		tot += u.Ntok
	}
	if s.N > 0 {
		s.Avg = float64(tot) / float64(s.N)
	}
	return s, nil
}

func LoadStoreOrSeed(path string) *Store {
	s, err := LoadStore(path)
	if err == nil && s != nil && len(s.U) > 0 {
		return s
	}
	s2 := NewStore()
	seedInto(s2)
	return s2
}
