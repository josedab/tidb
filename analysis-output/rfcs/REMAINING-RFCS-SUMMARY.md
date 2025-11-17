# Remaining RFCs - Summary

The following RFCs are outlined based on the codebase analysis. Full detailed versions can be developed as needed.

## RFC-0002: Adaptive Memory Spill Thresholds
**Status**: Design Phase
**Priority**: Strategic (High Impact)
**Effort**: 3-4 weeks

### Problem
Fixed memory thresholds for spill-to-disk operations cause:
- OOM kills when threshold too high
- Unnecessary disk I/O when threshold too low
- Poor adaptation to varying query complexity

### Solution
Implement adaptive thresholds based on:
- Current system memory pressure
- Query memory usage patterns
- Historical spill statistics

### Key Changes
- `pkg/executor/aggregate.go` - Dynamic spill decision
- `pkg/executor/sort.go` - Adaptive sort buffer sizing
- `pkg/util/memory/tracker.go` - Memory pressure signals

---

## RFC-0003: Query Hint Validation and Suggestions
**Status**: Ready to Implement
**Priority**: Quick Win
**Effort**: <1 week

### Problem
Typos in query hints silently ignored:
```sql
SELECT /*+ USE_INDEX(t, idx_name) */ * FROM t;  -- Works
SELECT /*+ USE_INDX(t, idx_name) */ * FROM t;   -- Silently ignored!
```

### Solution
- Parse hints during planning
- Validate hint names against whitelist
- Emit warnings for unknown hints
- Suggest corrections (Levenshtein distance)

### Key Changes
- `pkg/planner/core/planbuilder.go` - Hint validation
- `pkg/parser/ast/misc.go` - Hint parser enhancements

---

## RFC-0004: Background Statistics Refresh Optimization
**Status**: Design Phase
**Priority**: Strategic
**Effort**: 3-4 weeks

### Problem
Auto-analyze causes production latency spikes:
- Full table scans during peak hours
- No prioritization of frequently-queried tables
- Fixed schedule regardless of workload

### Solution
- Incremental statistics collection
- Query-driven prioritization
- Adaptive scheduling based on load

### Key Changes
- `pkg/statistics/handle/autoanalyze.go` - Scheduling logic
- `pkg/statistics/handle/update.go` - Incremental updates

---

## RFC-0005: Prepared Statement Cache Sharing
**Status**: Design Phase
**Priority**: Medium
**Effort**: 3 weeks

### Problem
Connection pools create duplicate cached plans:
- 100 connections × 50 cached plans = 5000 plans in memory
- Most are duplicates (same SQL, different connections)

### Solution
Global prepared statement cache with:
- Thread-safe access
- LRU eviction across all sessions
- Memory limits

### Key Changes
- `pkg/sessionctx/stmtctx/` - Shared cache layer
- `pkg/session/session.go` - Cache lookup logic

---

## RFC-0006: Error Message Improvement Framework
**Status**: Ready to Implement
**Priority**: Quick Win
**Effort**: 2 weeks

### Problem
Generic errors frustrate developers:
```
Error 1105: Out of memory quota!
```
Better version:
```
Error 1105: Out of memory quota! Query used 1.2GB, limit is 1GB.
Hint: Increase tidb_mem_quota_query or optimize query.
```

### Solution
Structured error framework with:
- Context fields (actual vs. limit)
- Actionable hints
- Links to documentation

### Key Changes
- `pkg/errno/errors.go` - Error definitions
- All error emission sites - Add context

---

## RFC-0007: Distributed Deadlock Detection
**Status**: Research Phase
**Priority**: Long-term
**Effort**: 12+ weeks

### Problem
Pessimistic lock deadlocks require manual intervention:
- No automatic detection across TiKV nodes
- Timeouts are only mitigation (poor UX)

### Solution
Distributed deadlock detection algorithm:
- Wait-for graph construction
- Cycle detection across cluster
- Automatic victim selection and rollback

### Complexity
- Requires coordination across TiDB + TiKV
- Edge cases: network partitions, node failures
- Performance overhead concerns

### Recommendation
- Full community RFC process required
- Prototype first on smaller scale

---

## RFC-0008: Index Selection Explainability
**Status**: Ready to Implement  
**Priority**: Quick Win
**Effort**: 2 weeks

### Problem
Optimizer index choices opaque:
```
EXPLAIN SELECT * FROM t WHERE a = 1 AND b = 2;
-- Why did it choose idx_a vs idx_b?
```

### Solution
Extended EXPLAIN output:
```
EXPLAIN FORMAT='verbose' SELECT ...;

Output:
├── IndexScan_10: idx_a (cost: 1250.5)
│   ├── Considered: idx_a (cost: 1250.5) ✓ chosen
│   ├── Considered: idx_b (cost: 2400.3) ✗ rejected (higher cost)
│   └── Considered: TableScan (cost: 50000) ✗ rejected (higher cost)
```

### Key Changes
- `pkg/planner/core/explain.go` - Verbose mode
- `pkg/planner/core/task.go` - Track rejected plans

---

## RFC-0010: Slow Query Log Sampling and Aggregation
**Status**: Ready to Implement
**Priority**: Quick Win
**Effort**: 2 weeks

### Problem
High-QPS systems overwhelmed by slow query logs:
- 10K slow queries/min = 600K/hour
- Disk I/O bottleneck
- Log rotation issues

### Solution
Sampling + aggregation:
- Sample 1/N slow queries (configurable)
- Aggregate similar queries (by digest)
- Periodic summary logs

### Key Changes
- `pkg/executor/adapter.go` - Sampling logic
- `pkg/util/logutil/slow_query_logger.go` - Aggregation

### Example Configuration
```toml
[log]
slow-query-sample-rate = 0.1  # Log 10% of slow queries
slow-query-aggregate = true    # Aggregate by digest
```

---

## Implementation Priority

### Immediate (Q1 2026)
- RFC-0003: Query Hint Validation
- RFC-0009: Chunk Pool Optimization (detailed above)
- RFC-0010: Slow Query Log Sampling
- RFC-0001: Plan Cache Warmup (detailed above)

### Next Quarter (Q2 2026)
- RFC-0002: Adaptive Memory Spill
- RFC-0004: Statistics Refresh
- RFC-0006: Error Messages
- RFC-0008: Index Explainability

### Future
- RFC-0005: Prepared Statement Sharing
- RFC-0007: Deadlock Detection (research phase)

---

**Note**: All RFCs based on analysis of commit [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
