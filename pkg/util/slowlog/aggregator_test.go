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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPatternAggregatorAdd(t *testing.T) {
	flushedStats := make([]*PatternStats, 0)
	var mu sync.Mutex

	flushFn := func(stats *PatternStats) {
		mu.Lock()
		defer mu.Unlock()
		flushedStats = append(flushedStats, stats)
	}

	// Create aggregator with long window to prevent auto-flush during test
	pa := NewPatternAggregator(1*time.Hour, flushFn)
	defer pa.Stop()

	// Add same query multiple times
	sql1 := "SELECT * FROM products WHERE id = 1"
	sql2 := "SELECT * FROM products WHERE id = 2"
	sql3 := "SELECT * FROM products WHERE id = 3"

	pa.Add(sql1, 100*time.Millisecond)
	pa.Add(sql2, 102*time.Millisecond)
	pa.Add(sql3, 98*time.Millisecond)

	// All should map to the same pattern
	hash := PatternHash(NormalizeQuery(sql1))
	require.True(t, pa.SeenBefore(hash), "Pattern should be seen")

	// Manually flush
	pa.Flush()

	// Should have one aggregated entry
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, len(flushedStats), "Should have one aggregated entry")

	stats := flushedStats[0]
	require.Equal(t, int64(3), stats.Count, "Count should be 3")
	require.Equal(t, 98*time.Millisecond, stats.MinDuration, "Min duration should be 98ms")
	require.Equal(t, 102*time.Millisecond, stats.MaxDuration, "Max duration should be 102ms")
}

func TestPatternAggregatorSeenBefore(t *testing.T) {
	pa := NewPatternAggregator(1*time.Hour, nil)
	defer pa.Stop()

	sql := "SELECT * FROM users WHERE id = 123"
	hash := PatternHash(NormalizeQuery(sql))

	// Not seen initially
	require.False(t, pa.SeenBefore(hash), "Pattern should not be seen initially")

	// Add query
	pa.Add(sql, 100*time.Millisecond)

	// Now it should be seen
	require.True(t, pa.SeenBefore(hash), "Pattern should be seen after adding")
}

func TestPatternStatsPercentile(t *testing.T) {
	stats := &PatternStats{
		Durations: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	}

	// Test P50 (median)
	p50 := stats.Percentile(50)
	require.InDelta(t, 0.5, p50.Seconds(), 0.1, "P50 should be around 0.5")

	// Test P95
	p95 := stats.Percentile(95)
	require.InDelta(t, 0.95, p95.Seconds(), 0.1, "P95 should be around 0.95")

	// Test P99
	p99 := stats.Percentile(99)
	require.InDelta(t, 0.99, p99.Seconds(), 0.1, "P99 should be around 0.99")
}

func TestPatternStatsAvgDuration(t *testing.T) {
	stats := &PatternStats{
		Count:         3,
		TotalDuration: 300 * time.Millisecond,
	}

	avg := stats.AvgDuration()
	require.Equal(t, 100*time.Millisecond, avg, "Average should be 100ms")
}

func TestPatternStatsFormatAggregatedLog(t *testing.T) {
	stats := &PatternStats{
		Pattern:       "SELECT * FROM products WHERE id = ?",
		Hash:          "abc123def456",
		Count:         100,
		Durations:     make([]float64, 100),
		MinDuration:   95 * time.Millisecond,
		MaxDuration:   150 * time.Millisecond,
		TotalDuration: 10 * time.Second,
		FirstSeen:     time.Now().Add(-1 * time.Minute),
		LastSeen:      time.Now(),
		SampleQuery:   "SELECT * FROM products WHERE id = 123",
	}

	// Fill durations with some values
	for i := 0; i < 100; i++ {
		stats.Durations[i] = 0.1 + float64(i)*0.0005
	}

	log := stats.FormatAggregatedLog()

	// Check that log contains expected fields
	require.Contains(t, log, "# Type: AGGREGATED")
	require.Contains(t, log, "# Pattern: SELECT * FROM products WHERE id = ?")
	require.Contains(t, log, "# Pattern_hash: abc123def456")
	require.Contains(t, log, "# Executions: 100")
	require.Contains(t, log, "# Sample_query: SELECT * FROM products WHERE id = 123")
	require.Contains(t, log, "# Min_duration:")
	require.Contains(t, log, "# Max_duration:")
	require.Contains(t, log, "# P50_duration:")
	require.Contains(t, log, "# P95_duration:")
	require.Contains(t, log, "# P99_duration:")
}

func TestPatternAggregatorMultiplePatterns(t *testing.T) {
	flushedStats := make([]*PatternStats, 0)
	var mu sync.Mutex

	flushFn := func(stats *PatternStats) {
		mu.Lock()
		defer mu.Unlock()
		flushedStats = append(flushedStats, stats)
	}

	pa := NewPatternAggregator(1*time.Hour, flushFn)
	defer pa.Stop()

	// Add different query patterns
	pa.Add("SELECT * FROM products WHERE id = 1", 100*time.Millisecond)
	pa.Add("SELECT * FROM users WHERE id = 1", 200*time.Millisecond)
	pa.Add("SELECT * FROM products WHERE id = 2", 105*time.Millisecond)

	// Flush
	pa.Flush()

	// Should have two aggregated entries (products and users)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, len(flushedStats), "Should have two aggregated entries")
}

func TestPatternAggregatorDisabled(t *testing.T) {
	pa := NewPatternAggregator(1*time.Hour, nil)
	defer pa.Stop()

	// Disable aggregator
	pa.SetEnabled(false)

	sql := "SELECT * FROM users WHERE id = 123"
	hash := PatternHash(NormalizeQuery(sql))

	// Add query
	pa.Add(sql, 100*time.Millisecond)

	// Should not be seen when disabled
	require.False(t, pa.SeenBefore(hash), "Pattern should not be tracked when disabled")
}

func TestPatternAggregatorConcurrency(t *testing.T) {
	flushedCount := 0
	var mu sync.Mutex

	flushFn := func(stats *PatternStats) {
		mu.Lock()
		defer mu.Unlock()
		flushedCount++
	}

	pa := NewPatternAggregator(1*time.Hour, flushFn)
	defer pa.Stop()

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				sql := "SELECT * FROM products WHERE id = " + string(rune('0'+j))
				pa.Add(sql, 100*time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	pa.Flush()

	// Should have processed all patterns without race conditions
	mu.Lock()
	defer mu.Unlock()
	require.Greater(t, flushedCount, 0, "Should have flushed some patterns")
}
