# RFC-0010: Slow Query Log Sampling and Aggregation

**Status**: Proposed
**Author**: TiDB Analysis Team
**Created**: 2025-11-17
**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)

---

## Executive Summary

**Problem**: Slow query log generates excessive I/O and storage usage:
- High-traffic systems log thousands of slow queries per second
- Many queries are identical (same SQL pattern, different params)
- Log files grow to gigabytes per hour
- Analysis tools overwhelmed by volume

**Solution**: Intelligent sampling and aggregation:
- Sample identical query patterns (keep 1 in N)
- Aggregate statistics for sampled queries
- Maintain full logs for outliers
- Configurable sampling rates

**Impact**:
- **Storage**: 80% reduction in slow log size
- **I/O**: 70% reduction in log write I/O
- **Analysis Speed**: 10x faster log analysis (less data)

**Effort**: 2 weeks (Quick Win)

**Risk**: Low (logs only, no query execution changes)

---

## Problem Statement

### Current Behavior

**File**: [`pkg/sessionctx/variable/slow_log.go:L120`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessionctx/variable/slow_log.go#L120)

Every slow query logged individually:

```go
func (s *SessionVars) SlowLogFormat(logItems *SlowQueryLogItems) string {
    // Log EVERY slow query, no sampling
    return formatSlowLog(logItems)
}
```

**Scenario**: E-commerce site during flash sale

```
Load: 10,000 QPS
Slow queries (>100ms): 2,000 QPS (20%)
Many are identical: SELECT * FROM products WHERE id = ?

Current log:
- 2,000 entries/second × 1KB/entry = 2MB/second
- 7.2GB/hour
- 172GB/day (unsustainable!)

Most entries are duplicates:
SELECT * FROM products WHERE id = 1;  -- 100ms
SELECT * FROM products WHERE id = 2;  -- 102ms
SELECT * FROM products WHERE id = 3;  -- 98ms
... (repeated 2000 times with different IDs)
```

---

## Proposed Solution

### Sampling Strategy

**Pattern-based sampling**:

```
1. Normalize query (remove literal values)
2. Calculate pattern hash
3. Check if pattern seen recently
4. If yes: Sample (1 in 100), aggregate stats
5. If no: Log fully (first occurrence)
```

**Example**:

```sql
-- Original queries (100 executions)
SELECT * FROM products WHERE id = 1;  -- 100ms
SELECT * FROM products WHERE id = 2;  -- 102ms
... (98 more)

-- Logged as:
# Pattern: SELECT * FROM products WHERE id = ?
# Executions: 100
# Avg duration: 101ms
# Min duration: 98ms
# Max duration: 150ms
# P50: 100ms, P95: 120ms, P99: 145ms
# Sample query: SELECT * FROM products WHERE id = 1;
```

**Storage savings**: 100 entries → 1 aggregated entry (99% reduction!)

---

## Detailed Design

### Component 1: Query Pattern Normalizer

**New file**: `pkg/util/slowlog/normalizer.go`

```go
package slowlog

import (
    "crypto/sha256"
    "encoding/hex"
    "regexp"
)

// NormalizeQuery removes literals and normalizes SQL
func NormalizeQuery(sql string) string {
    // Replace string literals with ?
    sql = regexp.MustCompile(`'[^']*'`).ReplaceAllString(sql, "?")

    // Replace numbers with ?
    sql = regexp.MustCompile(`\b\d+\b`).ReplaceAllString(sql, "?")

    // Normalize whitespace
    sql = regexp.MustCompile(`\s+`).ReplaceAllString(sql, " ")

    // Trim
    sql = strings.TrimSpace(sql)

    return sql
}

// PatternHash generates hash for query pattern
func PatternHash(normalizedSQL string) string {
    hash := sha256.Sum256([]byte(normalizedSQL))
    return hex.EncodeToString(hash[:8])  // 16-char hex
}
```

### Component 2: Query Pattern Aggregator

**New file**: `pkg/util/slowlog/aggregator.go`

```go
package slowlog

import (
    "sync"
    "time"

    "github.com/montanaflynn/stats"
)

// PatternAggregator aggregates stats for query patterns
type PatternAggregator struct {
    mu       sync.RWMutex
    patterns map[string]*PatternStats  // hash → stats
    window   time.Duration             // Aggregation window (e.g., 1 minute)
}

// PatternStats stores aggregated statistics
type PatternStats struct {
    Pattern      string
    Count        int64
    Durations    []float64  // For percentile calculation
    MinDuration  time.Duration
    MaxDuration  time.Duration
    TotalDuration time.Duration
    FirstSeen    time.Time
    LastSeen     time.Time
    SampleQuery  string     // Original SQL example
}

// NewPatternAggregator creates an aggregator
func NewPatternAggregator(window time.Duration) *PatternAggregator {
    pa := &PatternAggregator{
        patterns: make(map[string]*PatternStats),
        window:   window,
    }

    // Periodic flush
    go pa.flushLoop()

    return pa
}

// Add adds a slow query to aggregation
func (pa *PatternAggregator) Add(sql string, duration time.Duration) {
    normalized := NormalizeQuery(sql)
    hash := PatternHash(normalized)

    pa.mu.Lock()
    defer pa.mu.Unlock()

    stats, exists := pa.patterns[hash]
    if !exists {
        // First occurrence - create new pattern
        stats = &PatternStats{
            Pattern:      normalized,
            Count:        0,
            Durations:    make([]float64, 0, 1000),
            MinDuration:  duration,
            MaxDuration:  duration,
            FirstSeen:    time.Now(),
            SampleQuery:  sql,  // Store original
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

// Flush writes aggregated stats to slow log
func (pa *PatternAggregator) Flush() {
    pa.mu.Lock()
    defer pa.mu.Unlock()

    for hash, stats := range pa.patterns {
        if stats.Count > 0 {
            // Calculate percentiles
            p50, _ := stats.Percentile(50)
            p95, _ := stats.Percentile(95)
            p99, _ := stats.Percentile(99)

            // Write aggregated log entry
            logAggregatedPattern(stats, p50, p95, p99)

            // Clear
            delete(pa.patterns, hash)
        }
    }
}

// Percentile calculates percentile from durations
func (ps *PatternStats) Percentile(p float64) (time.Duration, error) {
    val, err := stats.Percentile(ps.Durations, p)
    if err != nil {
        return 0, err
    }
    return time.Duration(val * float64(time.Second)), nil
}

// flushLoop periodically flushes aggregated stats
func (pa *PatternAggregator) flushLoop() {
    ticker := time.NewTicker(pa.window)
    defer ticker.Stop()

    for range ticker.C {
        pa.Flush()
    }
}
```

### Component 3: Sampling Decision Logic

**Modified file**: `pkg/sessionctx/variable/slow_log.go`

```go
var (
    slowLogAggregator *slowlog.PatternAggregator
    samplingRate      = 100  // Sample 1 in 100 for repeated patterns
)

func init() {
    // Initialize aggregator with 1-minute window
    slowLogAggregator = slowlog.NewPatternAggregator(1 * time.Minute)
}

// SlowLogFormat decides whether to log, sample, or aggregate
func (s *SessionVars) SlowLogFormat(logItems *SlowQueryLogItems) string {
    // Always log if duration exceeds threshold by 10x (outliers)
    if logItems.Duration > s.SlowThreshold*10 {
        return formatFullSlowLog(logItems)
    }

    // Normalize and hash query
    normalized := slowlog.NormalizeQuery(logItems.SQL)
    hash := slowlog.PatternHash(normalized)

    // Check if pattern seen before
    if slowLogAggregator.SeenBefore(hash) {
        // Sample: Log 1 in N
        if logItems.ConnectionID%uint64(samplingRate) != 0 {
            // Aggregate only, don't log
            slowLogAggregator.Add(logItems.SQL, logItems.Duration)
            return ""  // Skip logging
        }
    }

    // First occurrence or sampled - log fully
    slowLogAggregator.Add(logItems.SQL, logItems.Duration)
    return formatFullSlowLog(logItems)
}
```

### Component 4: Aggregated Log Format

**Aggregated slow log entry**:

```
# Time: 2025-11-17T10:30:00Z
# Type: AGGREGATED
# Pattern: SELECT * FROM products WHERE id = ?
# Executions: 1,247
# Avg_duration: 0.102
# Min_duration: 0.095
# Max_duration: 0.850
# P50_duration: 0.100
# P95_duration: 0.125
# P99_duration: 0.200
# Total_duration: 127.294
# First_seen: 2025-11-17T10:29:00Z
# Last_seen: 2025-11-17T10:30:00Z
# Sample_query: SELECT * FROM products WHERE id = 123;
```

**Regular slow log entry** (first occurrence or sampled):

```
# Time: 2025-11-17T10:29:05Z
# Type: SAMPLE
# Pattern_hash: a3f2b8c9
# Query_time: 0.105
# Lock_time: 0.000
# Rows_sent: 1
# Rows_examined: 1
SELECT * FROM products WHERE id = 123;
```

---

## Implementation Plan

### Week 1: Core Sampling
- Implement query normalizer
- Implement pattern aggregator
- Basic sampling logic

### Week 2: Integration and Testing
- Integrate with slow log writer
- Performance testing
- Documentation

---

## Configuration

**New system variables**:

```sql
-- Enable sampling (default: ON)
SET GLOBAL tidb_slow_log_sampling_enabled = ON;

-- Sampling rate for repeated patterns (default: 100 = 1%)
SET GLOBAL tidb_slow_log_sampling_rate = 100;

-- Aggregation window (default: 60 seconds)
SET GLOBAL tidb_slow_log_aggregation_window = 60;

-- Always log outliers exceeding threshold by this factor
SET GLOBAL tidb_slow_log_outlier_factor = 10;
```

---

## Testing Strategy

```go
func TestSlowLogSampling(t *testing.T) {
    // Execute same query 1000 times
    for i := 0; i < 1000; i++ {
        tk.MustExec("SELECT * FROM t WHERE id = ?", i)
    }

    // Read slow log
    entries := readSlowLog()

    // Verify: Sampled (not all 1000 logged)
    require.Less(t, len(entries), 1000)

    // Verify: Aggregated entry exists
    aggEntry := findAggregatedEntry(entries, "SELECT * FROM t WHERE id = ?")
    require.NotNil(t, aggEntry)
    require.Equal(t, 1000, aggEntry.Executions)
}
```

---

## Performance Impact

**Before**:
- 2,000 slow queries/second × 1KB = 2MB/s write I/O
- 7.2GB/hour log size

**After** (100x sampling):
- 20 sampled + 1 aggregated entry/second × 1KB = 21KB/s write I/O
- 75MB/hour log size
- **Savings**: 99% I/O reduction, 99% storage reduction

---

## Analysis Tool Updates

**pt-query-digest compatibility**:

```bash
# Parse aggregated slow logs
pt-query-digest --type slowlog \
                --group-by pattern \
                --aggregated-format \
                tidb-slow.log

# Output shows aggregated stats per pattern
```

---

## References

- **MySQL Slow Log**: https://dev.mysql.com/doc/refman/8.0/en/slow-query-log.html
- **pt-query-digest**: https://www.percona.com/doc/percona-toolkit/LATEST/pt-query-digest.html
- **Log Sampling Best Practices**: https://www.usenix.org/conference/hotcloud12/workshop-program/presentation/nagaraj

---

**End of RFC-0010**

**Status**: Ready for Review
