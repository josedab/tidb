# Production-Grade Observability: Metrics, Logging, and Debugging

**Blog Series**: TiDB Deep Dive (Part 7 of 7)
**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
**Read Time**: 13 minutes

---

## What You'll Learn

- TiDB's comprehensive observability infrastructure
- Prometheus metrics (60+ families)
- Slow query log anatomy and analysis
- Distributed tracing with OpenTracing/Jaeger
- CPU and memory profiling
- Debugging techniques for production issues

---

## The Observability Stack

```
┌─────────────────────────────────────────────┐
│           Prometheus Metrics (60+)          │  ← What's happening now
├─────────────────────────────────────────────┤
│         Slow Query Log (60+ fields)         │  ← What happened (detailed)
├─────────────────────────────────────────────┤
│     Distributed Tracing (Jaeger)            │  ← Request flow across services
├─────────────────────────────────────────────┤
│         pprof Profiling (CPU/Mem)           │  ← Performance deep-dive
├─────────────────────────────────────────────┤
│           Top SQL Tracking                  │  ← Resource consumers
└─────────────────────────────────────────────┘
```

---

## Prometheus Metrics

**File**: [`pkg/metrics/`](https://github.com/pingcap/tidb/tree/bd6aa865/pkg/metrics)

### Key Metric Families

**Query Performance** [`pkg/metrics/server.go:50`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/metrics/server.go#L50):
```go
var (
    QueryDurationHistogram = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Namespace: "tidb",
            Subsystem: "server",
            Name:      "handle_query_duration_seconds",
            Help:      "Bucketed histogram of processing time (s) of handled queries.",
            Buckets:   prometheus.ExponentialBuckets(0.0005, 2, 29),  // 0.5ms to 1.5 days
        },
        []string{LblSQLType, LblDb, LblResourceGroup},
    )

    ExecuteErrorCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Namespace: "tidb",
            Subsystem: "server",
            Name:      "execute_error_total",
            Help:      "Counter of execute errors.",
        },
        []string{LblType},
    )

    ConnectionGauge = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Namespace: "tidb",
            Subsystem: "server",
            Name:      "connections",
            Help:      "Number of connections.",
        },
    )
)
```

**Plan Cache** [`pkg/metrics/server.go:200`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/metrics/server.go#L200):
```go
var (
    PlanCacheCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tidb_server_plan_cache_total",
            Help: "Counter of queries using plan cache.",
        },
        []string{"type"},  // hit, miss
    )

    PlanCacheMemoryUsage = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "tidb_server_plan_cache_instance_memory_usage",
            Help: "Memory usage of plan cache.",
        },
    )
)
```

**Memory** [`pkg/metrics/memory.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/metrics/memory.go):
```go
var (
    MemoryArbitrationDuration = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "tidb_memory_arbitration_duration_seconds",
            Help: "Duration of memory arbitration.",
        },
    )

    MemoryArbitratorQuota = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "tidb_memory_arbitrator_quota_bytes",
            Help: "Memory quota for arbitrator.",
        },
    )
)
```

### Accessing Metrics

**HTTP Endpoint**:
```bash
curl http://tidb-server:10080/metrics
```

**Output**:
```
# TYPE tidb_server_handle_query_duration_seconds histogram
tidb_server_handle_query_duration_seconds_bucket{sql_type="Select",db="test",le="0.001"} 1250
tidb_server_handle_query_duration_seconds_bucket{sql_type="Select",db="test",le="0.002"} 2340
tidb_server_handle_query_duration_seconds_sum{sql_type="Select",db="test"} 45.2
tidb_server_handle_query_duration_seconds_count{sql_type="Select",db="test"} 5000

# TYPE tidb_server_connections gauge
tidb_server_connections 342

# TYPE tidb_server_plan_cache_total counter
tidb_server_plan_cache_total{type="hit"} 8500
tidb_server_plan_cache_total{type="miss"} 1500
```

### Grafana Dashboards

**Pre-built dashboards**: [`pkg/metrics/grafana/`](https://github.com/pingcap/tidb/tree/bd6aa865/pkg/metrics/grafana)

**Panels**:
- Query duration (P50, P95, P99)
- QPS by SQL type
- Connection count
- Memory usage
- Plan cache hit rate

**Query example**:
```promql
# P99 query latency
histogram_quantile(0.99,
  sum(rate(tidb_server_handle_query_duration_seconds_bucket[5m])) by (le)
)

# Plan cache hit rate
sum(rate(tidb_server_plan_cache_total{type="hit"}[5m]))
/
sum(rate(tidb_server_plan_cache_total[5m]))
```

---

## Slow Query Log

**File**: [`pkg/sessionctx/variable/slow_log.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessionctx/variable/slow_log.go)

### Configuration

```sql
-- Enable slow query log
SET GLOBAL tidb_enable_slow_log = ON;

-- Threshold (milliseconds)
SET GLOBAL tidb_slow_log_threshold = 300;

-- Log file
SET GLOBAL tidb_slow_query_file = '/var/log/tidb/slow.log';

-- Include plan in slow log
SET GLOBAL tidb_record_plan_in_slow_log = ON;

-- Rate limiting
SET GLOBAL tidb_slow_log_max_per_sec = 100;  -- Max 100 slow queries/sec logged
```

### Slow Log Format

**File**: [`pkg/executor/adapter.go:600`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/adapter.go#L600)

**Example**:
```
# Time: 2025-11-17T10:30:45.123456789Z
# Txn_start_ts: 445678901234567890
# User@Host: app_user[app_user] @  [10.0.1.50]
# Conn_ID: 12345
# Query_time: 1.234567
# Parse_time: 0.000123
# Compile_time: 0.001234
# Rewrite_time: 0.000456
# Optimize_time: 0.002345
# Wait_TS: 0.000012
# Prewrite_time: 0.003456
# Commit_time: 0.001234
# Get_commit_ts_time: 0.000234
# Commit_backoff_time: 0.000000
# Backoff_types: []
# Resolve_lock_time: 0.000000
# Mem_max: 4194304  # 4MB
# Disk_max: 0
# Prepared: false
# Plan_from_cache: false
# Has_more_results: false
# Succ: true
# IsExplicitTxn: false
# IsWriteCacheTable: false
# Plan_digest: d08bc22f8f4e8c8e1b8f3e4d5c6b7a8f
# Prev_stmt: 
# Index_names: [users.idx_age,users.idx_city]
# Stats: users:pseudo
# Num_cop_tasks: 10
# Cop_proc_avg: 0.05 Cop_proc_p90: 0.08 Cop_proc_max: 0.12
# Cop_wait_avg: 0.001 Cop_wait_p90: 0.002 Cop_wait_max: 0.005
# Request_count: 10
# Total_keys: 50000
# Process_keys: 25000
# Rocksdb_block_cache_hit_count: 1200
# Rocksdb_block_read_byte: 10485760  # 10MB
# Rocksdb_block_read_count: 512
# DB: test
# Is_internal: false
# Digest: 83a123f4c567d890e1f2g3h4i5j6k7l8
# Result_rows: 100
# Warnings: []
SELECT name, email FROM users WHERE age > 30 AND city = 'NYC';
```

**60+ Fields!** Each provides insight into query execution

### Key Fields Explained

| Field | Meaning | Action if High |
|-------|---------|----------------|
| `Query_time` | Total execution time | Investigate plan, indexes |
| `Parse_time` | SQL parsing | Check for complex SQL |
| `Compile_time` | Plan building | Enable plan cache |
| `Optimize_time` | Query optimization | Check statistics freshness |
| `Mem_max` | Peak memory usage | Increase `tidb_mem_quota_query` or optimize |
| `Disk_max` | Temporary storage | Add memory or reduce result size |
| `Num_cop_tasks` | TiKV coprocessor tasks | Expected for distributed queries |
| `Process_keys` | Keys scanned | High → missing index |
| `Result_rows` | Rows returned | Much less than `Process_keys` → poor selectivity |

### Analyzing Slow Queries

**Parse slow log**:
```bash
# Extract slow queries
grep "Query_time:" /var/log/tidb/slow.log | awk '{print $3}' | sort -rn | head -10

# Find queries with high memory usage
grep "Mem_max:" /var/log/tidb/slow.log | awk '{if ($3 > 1073741824) print}' # >1GB
```

**Via SQL**:
```sql
-- Slow query table (parsed automatically)
SELECT 
    query_time,
    memory_max,
    query
FROM information_schema.slow_query
WHERE query_time > 1
ORDER BY query_time DESC
LIMIT 10;
```

---

## Distributed Tracing

**File**: [`pkg/util/tracing/`](https://github.com/pingcap/tidb/tree/bd6aa865/pkg/util/tracing)

### OpenTracing Integration

**Configuration**: [`pkg/config/config.go:209`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/config/config.go#L209)

```toml
[opentracing]
enable = true
rpc-metrics = true

[opentracing.sampler]
type = "probabilistic"  # or "const", "ratelimiting"
param = 0.1  # Sample 10% of traces

[opentracing.reporter]
queue-size = 1024
buffer-flush-interval = 1  # seconds
```

### Trace Span Creation

**File**: [`pkg/util/tracing/util.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/util/tracing/util.go)

```go
// Create span from context
func ChildSpanFromContext(ctx context.Context, opName string) (opentracing.Span, context.Context) {
    if sp := opentracing.SpanFromContext(ctx); sp != nil {
        child := opentracing.StartSpan(opName, opentracing.ChildOf(sp.Context()))
        return child, opentracing.ContextWithSpan(ctx, child)
    }
    return noopSpan(), ctx
}
```

**Example trace**:
```
Query Execution (1.2s total)
├── Parse (0.001s)
├── Plan (0.003s)
├── Execute (1.19s)
│   ├── TableReader (0.8s)
│   │   ├── TiKV Cop Request 1 (0.2s)
│   │   ├── TiKV Cop Request 2 (0.25s)
│   │   └── TiKV Cop Request 3 (0.3s)
│   └── HashAgg (0.35s)
└── EncodeResult (0.005s)
```

**Jaeger UI**: Visualizes trace timeline, dependencies

---

## Top SQL Tracking

**File**: [`pkg/domain/topn_slow_query.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/domain/topn_slow_query.go)

**Purpose**: Track CPU-heavy queries in real-time

**Data Structure**:
```go
type topNSlowQueries struct {
    recent   slowQueryQueue    // Recent N queries (FIFO)
    user     slowQueryHeap     // Top N user queries (by duration)
    internal slowQueryHeap     // Top N internal queries
    topN     int              // Default: 30
}
```

**Access**:
```sql
SELECT * FROM information_schema.statements_summary
ORDER BY sum_latency DESC
LIMIT 10;
```

**Output**:
```
+---------------+-----------+--------------+---------------+
| DIGEST_TEXT   | EXEC_COUNT| AVG_LATENCY  | SUM_LATENCY   |
+---------------+-----------+--------------+---------------+
| SELECT * F... | 50000     | 0.012s       | 600s          |
| UPDATE use... | 20000     | 0.008s       | 160s          |
+---------------+-----------+--------------+---------------+
```

---

## CPU Profiling

**File**: [`pkg/util/cpuprofile/cpuprofile.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/util/cpuprofile/cpuprofile.go)

### HTTP Endpoints

**pprof available at**: `http://tidb-server:10080/debug/pprof/`

**Profiles**:
```bash
# CPU profile (30 seconds)
curl http://tidb-server:10080/debug/pprof/profile?seconds=30 > cpu.prof

# Heap profile
curl http://tidb-server:10080/debug/pprof/heap > heap.prof

# Goroutine profile
curl http://tidb-server:10080/debug/pprof/goroutine?debug=2

# Mutex contention
curl http://tidb-server:10080/debug/pprof/mutex > mutex.prof
```

### Analyzing Profiles

```bash
# Install pprof
go install github.com/google/pprof@latest

# Interactive analysis
pprof -http=:8080 cpu.prof

# Top functions by CPU
pprof -top cpu.prof

# Flamegraph
pprof -flame cpu.prof > flame.svg
```

**Example output**:
```
(pprof) top
Showing top 10 nodes out of 245
      flat  flat%   sum%        cum   cum%
   1250ms 25.00% 25.00%     1800ms 36.00%  runtime.scanobject
    800ms 16.00% 41.00%      800ms 16.00%  runtime.mallocgc
    500ms 10.00% 51.00%      500ms 10.00%  github.com/pingcap/tidb/pkg/executor.(*HashJoinExec).Next
```

---

## Memory Profiling

**File**: [`pkg/util/profile/profile.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/util/profile/profile.go)

### Via SQL

```sql
-- Get heap profile
SELECT * FROM information_schema.tidb_profile
WHERE type = 'heap';
```

### Memory Tracking

**Per-query**:
```sql
SELECT 
    CONN_ID,
    MEM,
    SQL_DIGEST
FROM information_schema.PROCESSLIST
WHERE MEM > 1073741824;  -- >1GB
```

**Global**:
```sql
SHOW VARIABLES LIKE '%memory%';
```

---

## Flight Recorder

**File**: [`pkg/util/traceevent/flightrecorder.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/util/traceevent/flightrecorder.go)

**Purpose**: Circular buffer for post-mortem analysis

**Configuration**:
```toml
[trace-event]
mode = "base"  # "off", "base", "full"
capacity = 1024  # Events in buffer
```

**Modes**:
- **off**: Disabled
- **base**: Flight recorder only (no logging)
- **full**: Flight recorder + log emission

**Triggered by**: Slow queries, failures, manual dump

**Dump**:
```sql
-- Trigger flight recorder dump
ADMIN TRACE DUMP;
```

---

## Debugging Techniques

### 1. Query Not Using Index

**Symptom**: High `Process_keys` in slow log

**Diagnosis**:
```sql
EXPLAIN SELECT * FROM users WHERE email = 'alice@example.com';
```

**If showing TableScan instead of IndexScan**:
```sql
-- Create index
CREATE INDEX idx_email ON users(email);

-- Or force index usage
SELECT /*+ USE_INDEX(users, idx_email) */ * FROM users WHERE email = 'alice@example.com';
```

### 2. High Memory Usage

**Symptom**: `Mem_max` > quota in slow log

**Diagnosis**:
```sql
-- Check query plan
EXPLAIN ANALYZE SELECT ...;
```

**Fix**:
```sql
-- Increase memory quota
SET SESSION tidb_mem_quota_query = 2147483648;  -- 2GB

-- Or optimize query (add filters, use indexes)
```

### 3. Slow Aggregation

**Symptom**: `HashAgg` executor taking long time

**Diagnosis**: Check if sorted input available

**Fix**:
```sql
-- If data sorted by GROUP BY columns, stream agg is faster
SELECT /*+ STREAM_AGG() */ region, COUNT(*)
FROM orders
GROUP BY region;
```

### 4. Lock Waits

**Symptom**: Long `Wait_TS` in slow log

**Diagnosis**:
```sql
-- Check active transactions
SELECT * FROM information_schema.tidb_trx
WHERE STATE = 'Running';
```

**Fix**:
- Use pessimistic transactions for high-contention workloads
- Keep transactions short

---

## Monitoring Best Practices

1. **Set up alerts**:
   ```yaml
   # Prometheus alert rules
   - alert: HighQueryLatency
     expr: histogram_quantile(0.99, tidb_server_handle_query_duration_seconds_bucket) > 1
     for: 5m
     annotations:
       summary: "P99 query latency > 1s"
   ```

2. **Monitor plan cache hit rate**:
   ```promql
   # Should be >80% for cacheable workloads
   sum(rate(tidb_server_plan_cache_total{type="hit"}[5m]))
   /
   sum(rate(tidb_server_plan_cache_total[5m]))
   ```

3. **Track memory usage**:
   ```sql
   SELECT 
       SUM(MEM) as total_memory_mb
   FROM information_schema.PROCESSLIST;
   ```

4. **Review slow queries daily**:
   ```sql
   SELECT query_time, query
   FROM information_schema.slow_query
   WHERE time > DATE_SUB(NOW(), INTERVAL 24 HOUR)
   ORDER BY query_time DESC
   LIMIT 20;
   ```

---

## Key Takeaways

1. **60+ Prometheus metrics** cover all aspects of performance
2. **Slow query log** with 60+ fields for detailed analysis
3. **Distributed tracing** via OpenTracing for cross-service visibility
4. **CPU/memory profiling** via pprof endpoints
5. **Flight recorder** for post-mortem debugging
6. **Top SQL** tracks resource-heavy queries

---

## Conclusion

This concludes our 7-part deep dive into TiDB! You've learned:
- **Part 1**: Architecture and core concepts
- **Part 2**: Query execution pipeline
- **Part 3**: Query optimization internals
- **Part 4**: Distributed transactions
- **Part 5**: Execution engine
- **Part 6**: Online DDL
- **Part 7**: Observability (this post)

**Next Steps**:
- Explore the [RFCs](../rfcs/) for improvement opportunities
- Read [Initial Analysis](../initial-analysis/) for detailed metrics
- Contribute to TiDB: https://github.com/pingcap/tidb

---

**Part 7 of 7 - Series Complete!**
