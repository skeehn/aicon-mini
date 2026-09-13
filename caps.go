package main

import (
	"fmt"
	"time"
)

type CapConfig struct {
	Daily   int
	Monthly int
	PerTask int
}

type SpendTracker struct {
	Daily       map[string]int
	Monthly     map[string]int
	PerTask     map[string]int
	LoopHistory map[string][]int64
	Grants      map[string]int
}

func NewSpendTracker() *SpendTracker {
	return &SpendTracker{
		Daily:       map[string]int{},
		Monthly:     map[string]int{},
		PerTask:     map[string]int{},
		LoopHistory: map[string][]int64{},
		Grants:      map[string]int{},
	}
}

var capsDB = map[string]CapConfig{
	"default": {Daily: 10000, Monthly: 100000, PerTask: 500},
}

func GetCap(key string) CapConfig {
	if c, ok := capsDB[key]; ok {
		return c
	}
	return capsDB["default"]
}

func (s *SpendTracker) CheckAndReserve(key string, estimatedCents int, cap CapConfig) bool {
	today := time.Now().Format("20060102")
	month := time.Now().Format("200601")
	dailyKey := key + ":daily:" + today
	monthlyKey := key + ":monthly:" + month
	if s.Daily[dailyKey]+estimatedCents > cap.Daily {
		return false
	}
	if s.Monthly[monthlyKey]+estimatedCents > cap.Monthly {
		return false
	}
	s.Daily[dailyKey] += estimatedCents
	s.Monthly[monthlyKey] += estimatedCents
	s.PerTask[key] += estimatedCents
	if s.PerTask[key] > cap.PerTask {
		s.PerTask[key] -= estimatedCents
		s.Daily[dailyKey] -= estimatedCents
		s.Monthly[monthlyKey] -= estimatedCents
		return false
	}
	return true
}

func (s *SpendTracker) CheckLoop(taskID string) bool {
	now := time.Now().Unix()
	history := s.LoopHistory[taskID]
	var recent []int64
	for _, t := range history {
		if now-t < 60 {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	s.LoopHistory[taskID] = recent
	return len(recent) < 5
}

func (s *SpendTracker) CreateGrant(grantID string, cents int, ttlSeconds int) {
	s.Grants[grantID] = cents
}

func (s *SpendTracker) UseGrant(grantID string) (int, bool) {
	cents, ok := s.Grants[grantID]
	if ok {
		delete(s.Grants, grantID)
		return cents, true
	}
	return 0, false
}

func FormatCapError(key string, cap CapConfig, spent int) string {
	return fmt.Sprintf("402 Payment Required: cap exceeded for %s (cap daily=%d monthly=%d per_task=%d spent=%d)", key, cap.Daily, cap.Monthly, cap.PerTask, spent)
}

func CapUsedPercent(tracker *SpendTracker, key string) float64 {
	today := time.Now().Format("20060102")
	dailyKey := key + ":daily:" + today
	cap := GetCap(key)
	if cap.Daily == 0 {
		return 0
	}
	return float64(tracker.Daily[dailyKey]) / float64(cap.Daily) * 100
}
