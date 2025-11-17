// Copyright 2025 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package slowlog

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// PatternAggregator aggregates statistics for slow query patterns.
// It collects execution statistics for identical query patterns and
// periodically flushes aggregated data to the slow log.
type PatternAggregator struct {
	mu       sync.RWMutex
	patterns map[string]*PatternStats // hash → stats
	window   time.Duration            // Aggregation window (e.g., 1 minute)
	enabled  bool                     // Whether aggregation is enabled
	ticker   *time.Ticker
	stopCh   chan struct{}
	flushFn  func(*PatternStats)
}

// PatternStats stores aggregated statistics for a query pattern.
type PatternStats struct {
	Pattern       string
	Hash          string
	Count         int64
	Durations     []float64 // For percentile calculation (in seconds)
	MinDuration   time.Duration
	MaxDuration   time.Duration
	TotalDuration time.Duration
	FirstSeen     time.Time
	LastSeen      time.Time
	SampleQuery   string // Original SQL example
}

// NewPatternAggregator creates a new aggregator with the specified window.
// The flushFn is called for each pattern when the window expires.
func NewPatternAggregator(window time.Duration, flushFn func(*PatternStats)) *PatternAggregator {
	if window <= 0 {
		window = 60 * time.Second // Default: 1 minute
	}

	pa := &PatternAggregator{
		patterns: make(map[string]*PatternStats),
		window:   window,
		enabled:  true,
		stopCh:   make(chan struct{}),
		flushFn:  flushFn,
	}

	// Start periodic flush
	pa.startFlushLoop()

	return pa
}

// Add adds a slow query to the aggregation.
// If this is the first occurrence of the pattern, it creates a new entry.
// Otherwise, it updates the existing statistics.
func (pa *PatternAggregator) Add(sql string, duration time.Duration) {
	if !pa.enabled {
		return
	}

	normalized := NormalizeQuery(sql)
	hash := PatternHash(normalized)

	pa.mu.Lock()
	defer pa.mu.Unlock()

	stats, exists := pa.patterns[hash]
	if !exists {
		// First occurrence - create new pattern
		stats = &PatternStats{
			Pattern:       normalized,
			Hash:          hash,
			Count:         0,
			Durations:     make([]float64, 0, 1000),
			MinDuration:   duration,
			MaxDuration:   duration,
			TotalDuration: 0,
			FirstSeen:     time.Now(),
			SampleQuery:   sql, // Store original
		}
		pa.patterns[hash] = stats
	}

	// Update statistics
	stats.Count++
	stats.Durations = append(stats.Durations, duration.Seconds())
	stats.TotalDuration += duration
	stats.LastSeen = time.Now()

	if duration < stats.MinDuration {
		stats.MinDuration = duration
	}
	if duration > stats.MaxDuration {
		stats.MaxDuration = duration
	}
}

// SeenBefore checks if a pattern has been seen before.
// Returns true if the pattern already exists in the aggregator.
func (pa *PatternAggregator) SeenBefore(hash string) bool {
	if !pa.enabled {
		return false
	}

	pa.mu.RLock()
	defer pa.mu.RUnlock()

	_, exists := pa.patterns[hash]
	return exists
}

// Flush writes aggregated stats to slow log and clears the patterns.
func (pa *PatternAggregator) Flush() {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	for hash, stats := range pa.patterns {
		if stats.Count > 0 && pa.flushFn != nil {
			// Make a copy to avoid holding the lock during flush
			statsCopy := *stats
			pa.flushFn(&statsCopy)
		}
		// Clear the pattern
		delete(pa.patterns, hash)
	}
}

// startFlushLoop starts the periodic flush goroutine.
func (pa *PatternAggregator) startFlushLoop() {
	pa.ticker = time.NewTicker(pa.window)

	go func() {
		for {
			select {
			case <-pa.ticker.C:
				pa.Flush()
			case <-pa.stopCh:
				return
			}
		}
	}()
}

// Stop stops the aggregator and flushes remaining data.
func (pa *PatternAggregator) Stop() {
	if pa.ticker != nil {
		pa.ticker.Stop()
	}
	close(pa.stopCh)
	pa.Flush()
}

// SetEnabled enables or disables the aggregator.
func (pa *PatternAggregator) SetEnabled(enabled bool) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.enabled = enabled
}

// SetWindow updates the aggregation window.
func (pa *PatternAggregator) SetWindow(window time.Duration) {
	if window <= 0 {
		return
	}

	pa.mu.Lock()
	defer pa.mu.Unlock()

	pa.window = window
	if pa.ticker != nil {
		pa.ticker.Stop()
		pa.ticker = time.NewTicker(window)
	}
}

// Percentile calculates the p-th percentile from the durations.
// p should be between 0 and 100 (e.g., 50 for median, 95 for p95).
func (ps *PatternStats) Percentile(p float64) time.Duration {
	if len(ps.Durations) == 0 {
		return 0
	}

	// Sort durations (create a copy to avoid modifying original)
	sorted := make([]float64, len(ps.Durations))
	copy(sorted, ps.Durations)
	sort.Float64s(sorted)

	// Calculate index
	index := (p / 100.0) * float64(len(sorted)-1)
	lower := int(index)
	upper := lower + 1

	if upper >= len(sorted) {
		return time.Duration(sorted[len(sorted)-1] * float64(time.Second))
	}

	// Linear interpolation
	fraction := index - float64(lower)
	value := sorted[lower] + fraction*(sorted[upper]-sorted[lower])

	return time.Duration(value * float64(time.Second))
}

// AvgDuration returns the average duration.
func (ps *PatternStats) AvgDuration() time.Duration {
	if ps.Count == 0 {
		return 0
	}
	return ps.TotalDuration / time.Duration(ps.Count)
}

// FormatAggregatedLog formats the aggregated statistics as a slow log entry.
func (ps *PatternStats) FormatAggregatedLog() string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("# Time: %s\n", ps.LastSeen.Format("2006-01-02T15:04:05.000000Z07:00")))
	buf.WriteString("# Type: AGGREGATED\n")
	buf.WriteString(fmt.Sprintf("# Pattern: %s\n", ps.Pattern))
	buf.WriteString(fmt.Sprintf("# Pattern_hash: %s\n", ps.Hash))
	buf.WriteString(fmt.Sprintf("# Executions: %d\n", ps.Count))
	buf.WriteString(fmt.Sprintf("# Avg_duration: %.6f\n", ps.AvgDuration().Seconds()))
	buf.WriteString(fmt.Sprintf("# Min_duration: %.6f\n", ps.MinDuration.Seconds()))
	buf.WriteString(fmt.Sprintf("# Max_duration: %.6f\n", ps.MaxDuration.Seconds()))
	buf.WriteString(fmt.Sprintf("# P50_duration: %.6f\n", ps.Percentile(50).Seconds()))
	buf.WriteString(fmt.Sprintf("# P95_duration: %.6f\n", ps.Percentile(95).Seconds()))
	buf.WriteString(fmt.Sprintf("# P99_duration: %.6f\n", ps.Percentile(99).Seconds()))
	buf.WriteString(fmt.Sprintf("# Total_duration: %.6f\n", ps.TotalDuration.Seconds()))
	buf.WriteString(fmt.Sprintf("# First_seen: %s\n", ps.FirstSeen.Format("2006-01-02T15:04:05.000000Z07:00")))
	buf.WriteString(fmt.Sprintf("# Last_seen: %s\n", ps.LastSeen.Format("2006-01-02T15:04:05.000000Z07:00")))
	buf.WriteString(fmt.Sprintf("# Sample_query: %s\n", ps.SampleQuery))

	return buf.String()
}
