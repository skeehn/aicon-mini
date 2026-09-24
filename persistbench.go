package main

// persistbench.go: SQLite (modernc/WAL) vs JSON snapshot - latency, size, restart cost.
// Winner becomes the default STORE_PATH backend. Run: go run . persistbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runPersistBench() int {
	dir := filepath.Join("", fmt.Sprintf("pb-%s", time.Now().Format("150405")))
	_ = os.MkdirAll(dir, 0755)
	s := NewStore()
	seedInto(s)
	start := time.Now()
	for i := 0; i < 500; i++ {
		s.Ingest("bench-src", "Persistence benchmark content chunk "+strings.Repeat("padding ", 10)+fmt.Sprint(i), "docs", 2026)
	}
	ingest0 := time.Since(start)

	start = time.Now()
	_ = s.Save(filepath.Join(dir, "store.json"))
	save0 := time.Since(start)
	lStart := time.Now()
	if _, e := LoadStore(filepath.Join(dir, "store.json")); e != nil {
	}
	load0 := time.Since(lStart)
	jSize := fileSize(filepath.Join(dir, "store.json"))

	db, err2 := OpenSQLite(filepath.Join(dir, "store.db"))
	if err2 != nil {
		fmt.Println("sqlite open failed:", err2)
		return 1
	}
	defer db.Close()
	start = time.Now()
	for _, u := range s.U {
		db.IngestUnit(u, false, "")
	}
	sqlSave := time.Since(start)
	sSize := fileSize(filepath.Join(dir, "store.db"))
	sStart := time.Now()
	s2 := NewStore()
	if err3 := db.LoadInto(s2); err3 != nil {
	}
	sqlLoad := time.Since(sStart)
	fileSize(filepath.Join(dir, "store.db"))

	fmt.Printf("== PERSISTENCE BENCH (seed + %d new units) ==\n", 500)
	fmt.Printf("ingest: %v\n", ingest0)
	fmt.Printf("json save: %v (%d bytes)\n", save0, jSize)
	fmt.Printf("json load: %v\n", load0)
	fmt.Printf("sqlite batch save: %v (%d bytes)\n", sqlSave, sSize)
	fmt.Printf("sqlite load: %v\n", sqlLoad)
	winner := "json"
	if sqlSave+sqlLoad < save0+load0 {
		winner = "sqlite"
	}
	fmt.Printf("WINNER: %s (json %.1fms vs sqlite %.1fms roundtrip)\n", winner, float64((save0+load0).Microseconds())/1000, float64((sqlSave+sqlLoad).Microseconds())/1000)
	return 0
}

func fileSize(p string) int64 {
	b, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return b.Size()
}
