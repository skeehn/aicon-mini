package main

// persist_sqlite.go: SQLite backing store (modernc.org/sqlite - pure Go, no cgo).
// Durable, WAL mode, concurrent-safe. Compare with JSON snapshot via persist bench.

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	DB *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	if path == "" {
		path = "aicon.db"
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS units (
		id TEXT PRIMARY KEY, source TEXT, text TEXT, provenance TEXT,
		ntokens INTEGER, level INTEGER, year INTEGER,
		deleted INTEGER DEFAULT 0, superseded_by TEXT DEFAULT '',
		first_ingested_at INTEGER, updated_at INTEGER)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_source ON units(source)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS edges (
		from_id TEXT, to_id TEXT, rel TEXT, weight REAL);`); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{DB: db}, nil
}

func (s *SQLiteStore) IngestUnit(u *Unit, tomb bool, supBy string) error {
	_, err := s.DB.Exec(`INSERT INTO units (id, source, text, provenance, ntokens, level, year, deleted, superseded_by, first_ingested_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), unixepoch())
		ON CONFLICT(id) DO UPDATE SET updated_at=unixepoch()`,
		u.ID, u.Src, u.Text, u.Prov, u.Ntok, u.Level, u.Time, 0, supBy)
	return err
}

func (s *SQLiteStore) Tombstone(unitID, supBy string) error {
	_, err := s.DB.Exec(`UPDATE units SET deleted=1, superseded_by=?, updated_at=unixepoch() WHERE id=?`, supBy, unitID)
	return err
}

func (s *SQLiteStore) LoadInto(s2 *Store) error {
	rows, err := s.DB.Query(`SELECT id, source, text, provenance, ntokens, level, year FROM units WHERE deleted=0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, src, text, prov string
		var ntok, level int
		var yr int64
		if err := rows.Scan(&id, &src, &text, &prov, &ntok, &level, &yr); err != nil {
			return err
		}
		u := &Unit{ID: id, Src: src, Text: text, Prov: prov, Tok: tok(text), Vec: vecOfHash(tok(text)), Ntok: ntok, Time: yr, Level: level}
		s2.U = append(s2.U, u)
		s2.ByID[u.ID] = u
		seen := map[string]bool{}
		for _, w := range u.Tok {
			if !seen[w] {
				s2.DF[w]++
				seen[w] = true
			}
		}
	}
	s2.N = len(s2.U)
	s2.Assoc.N = len(s2.U)
	tot := 0
	for _, u := range s2.U {
		tot += u.Ntok
	}
	if s2.N > 0 {
		s2.Avg = float64(tot) / float64(s2.N)
	}
	return rows.Err()
}

func (s *SQLiteStore) IngestText(src, text, prov string, year int64) ([]string, error) {
	ids := NewStore().Ingest(src, text, prov, year)
	for _, id := range ids {
		if _, err := s.DB.Exec(`INSERT INTO units (id, source, text, provenance, ntokens, level, year, deleted, superseded_by, first_ingested_at, updated_at) VALUES (?,?,?,?,(SELECT count(sent) FROM (SELECT 1 as sent)),0,?,0,'',unixepoch(),unixepoch())`, id, src, text, prov, year); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

func (s *SQLiteStore) Close() error { return s.DB.Close() }
