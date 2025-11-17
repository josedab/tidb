# Query Optimization Secrets: Plans, Statistics, and Costing

**Blog Series**: TiDB Deep Dive (Part 3 of 7)
**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
**Read Time**: 16 minutes

---

## What You'll Learn

- How TiDB's query optimizer makes decisions
- Rule-based vs. cost-based optimization
- How statistics drive the optimizer
- Join reordering algorithms
- Plan caching strategies and when they help
- Common optimization pitfalls and how to avoid them

---

## The Optimizer's Job

Given a SQL query, the optimizer must answer:
1. **Which indexes to use?** (if any)
2. **In what order to join tables?**
3. **Where to apply filters?** (TiDB vs. TiKV)
4. **How to aggregate?** (hash vs. stream)

**Goal**: Minimize query execution time

**Challenge**: Exponential search space
- 3 tables = 12 possible join orders
- 10 tables = 3,628,800 possible join orders!

---

## Two-Phase Optimization

**File**: [`pkg/planner/core/optimizer.go:68`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/optimizer.go#L68)

```go
func Optimize(ctx context.Context, sctx sessionctx.Context, node ast.Node) (Plan, error) {
    // Phase 1: Logical optimization (rule-based)
    logic, err := logicalOptimize(ctx, logic, sctx)

    // Phase 2: Physical optimization (cost-based)
    physical, cost, err := physicalOptimize(logic, sctx)

    return physical, nil
}
```

---

## Phase 1: Logical Optimization (Rule-Based)

### The Rule Pipeline

**File**: [`pkg/planner/core/optimizer.go:87`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/optimizer.go#L87)

```go
var optRuleList = []logicalOptRule{
    &columnPruner{},                 // 1. Remove unused columns
    &buildKeySolver{},               // 2. Determine unique keys
    &decorrelateSolver{},            // 3. Unnest correlated subqueries
    &aggregationEliminator{},        // 4. Remove redundant GROUP BY
    &projectionEliminator{},         // 5. Remove redundant projections
    &maxMinEliminator{},             // 6. MAX/MIN → index scan
    &ppdSolver{},                    // 7. Predicate push down
    &outerJoinEliminator{},          // 8. Outer → inner join
    &partitionProcessor{},           // 9. Partition pruning
    &aggregationPushDownSolver{},    // 10. Agg push down
    &pushDownTopNOptimizer{},        // 11. TopN push down
    &joinReOrderSolver{},            // 12. Join reordering (DP/greedy)
    &columnPruner{},                 // 13. Column pruning (again)
    &buildKeySolver{},               // 14. Rebuild keys
    &resolveExpand{},                // 15. Resolve expand operators
}
```

**Each rule runs in sequence**, transforming the logical plan.

### Rule 1: Column Pruning

**Purpose**: Fetch only needed columns

**File**: [`pkg/planner/core/rule_column_pruning.go:50`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/rule_column_pruning.go#L50)

**Example**:
```sql
SELECT name FROM users WHERE age > 20;
```

**Before**:
```
DataSource (fetch all 20 columns from users table)
```

**After**:
```
DataSource (fetch only: id, name, age)
```

**Why `id` and `age`?**
- `name`: In SELECT clause
- `age`: In WHERE clause
- `id`: Primary key (needed for row identification)

**Code**:
```go
func (p *LogicalProjection) PruneColumns(parentUsedCols []*expression.Column) {
    // Only fetch columns needed by parent + own expressions
    usedCols := extractColumns(p.Exprs)
    usedCols = append(usedCols, parentUsedCols...)

    p.children[0].PruneColumns(usedCols)
}
```

**Benefit**: 90% reduction in data transfer for wide tables

---

### Rule 7: Predicate Push-Down (PPD)

**Purpose**: Apply filters as early as possible

**File**: [`pkg/planner/core/rule_predicate_push_down.go:100`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/rule_predicate_push_down.go#L100)

**Example**:
```sql
SELECT * FROM orders o
JOIN customers c ON o.customer_id = c.id
WHERE o.amount > 1000;
```

**Before**:
```
Selection (amount > 1000)
    ↓
Join
    ├── orders (full table)
    └── customers (full table)
```

**After**:
```
Join
    ├── orders (filtered: amount > 1000)  ← Filter pushed down!
    └── customers
```

**Benefit**: If 90% of orders are <$1000, we scan 10x less data

**Code**:
```go
func (p *LogicalJoin) PredicatePushDown(predicates []expression.Expression) []expression.Expression {
    leftCond, rightCond := extractJoinPredicates(predicates, p.LeftChild, p.RightChild)

    // Push to left child
    p.LeftChild.PredicatePushDown(leftCond)

    // Push to right child
    p.RightChild.PredicatePushDown(rightCond)

    return remainingPredicates
}
```

**Push-Down to TiKV**:

When filter can be evaluated on TiKV:

```
TiDB: SELECT * FROM orders WHERE amount > 1000
    ↓
TiKV Coprocessor: Applies filter, returns only matching rows
```

**Conditions for push-down**:
- ✅ Simple comparisons (`>`, `=`, `LIKE`)
- ✅ AND/OR combinations
- ❌ Non-deterministic functions (`NOW()`, `RAND()`)
- ❌ User-defined functions

---

### Rule 6: MAX/MIN Elimination

**Purpose**: Convert aggregations to index lookups

**Example**:
```sql
SELECT MAX(id) FROM users;
```

**Naive Plan**:
```
Aggregation (MAX)
    ↓
TableScan (read all rows)
```

**Optimized Plan** (if `id` is indexed):
```
Limit 1
    ↓
IndexScan (id DESC)  ← Just read first row!
```

**Code**: [`pkg/planner/core/rule_max_min_eliminate.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/rule_max_min_eliminate.go)

**Speedup**: 1000x for large tables (1 row vs. 1M rows)

---

### Rule 12: Join Reordering

**Purpose**: Find optimal join order

**Problem**: Given tables A, B, C:
```
Possible orders:
1. (A JOIN B) JOIN C
2. (A JOIN C) JOIN B
3. (B JOIN A) JOIN C
4. (B JOIN C) JOIN A
5. (C JOIN A) JOIN B
6. (C JOIN B) JOIN A
```

**How to choose?** Cost-based decision!

**File**: [`pkg/planner/core/rule_join_reorder.go:80`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/rule_join_reorder.go#L80)

```go
func (s *joinReOrderSolver) optimize(ctx context.Context, p LogicalPlan) (LogicalPlan, error) {
    // Count tables in join
    if numTables < 7 {
        // Use Dynamic Programming (exact)
        return s.dpOptimize(ctx, p)
    } else {
        // Use Greedy (heuristic)
        return s.greedyOptimize(ctx, p)
    }
}
```

**Dynamic Programming** (for ≤6 tables):

```go
func (s *joinReOrderSolver) dpOptimize(joinGroup []LogicalPlan) LogicalPlan {
    n := len(joinGroup)
    // dp[S] = best plan for subset S
    dp := make(map[uint64]*LogicalJoin)

    // Enumerate all subsets
    for subset := 1; subset < (1 << n); subset++ {
        // Try all ways to split subset into two parts
        for left := subset; left > 0; left = (left - 1) & subset {
            right := subset ^ left

            // Cost of joining left and right
            leftPlan := dp[left]
            rightPlan := dp[right]
            cost := calculateJoinCost(leftPlan, rightPlan)

            if cost < dp[subset].Cost {
                dp[subset] = makeJoin(leftPlan, rightPlan)
            }
        }
    }

    return dp[(1<<n) - 1]  // Plan for all tables
}
```

**Complexity**: O(3^n) - feasible for n ≤ 6

**Greedy Algorithm** (for ≥7 tables):

```go
func (s *joinReOrderSolver) greedyOptimize(tables []LogicalPlan) LogicalPlan {
    result := tables[0]
    remaining := tables[1:]

    for len(remaining) > 0 {
        bestNext := 0
        bestCost := math.MaxFloat64

        // Find cheapest table to join next
        for i, table := range remaining {
            cost := calculateJoinCost(result, table)
            if cost < bestCost {
                bestNext = i
                bestCost = cost
            }
        }

        result = makeJoin(result, remaining[bestNext])
        remaining = remove(remaining, bestNext)
    }

    return result
}
```

**Why Greedy?** O(n²) - fast enough for 100+ tables

---

## Phase 2: Physical Optimization (Cost-Based)

### Cost Model

**File**: [`pkg/planner/core/task.go:100`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/task.go#L100)

```go
type costVer2 struct {
    scanCost float64     // I/O cost
    netCost  float64     // Network transfer
    cpuCost  float64     // CPU processing
    memCost  float64     // Memory usage
}

func (c *costVer2) total() float64 {
    // Weighted sum of costs
    return c.scanCost*scanFactor +
           c.netCost*netFactor +
           c.cpuCost*cpuFactor +
           c.memCost*memFactor
}
```

**Cost Factors**:
- **Scan Cost**: Disk I/O (proportional to row count)
- **Network Cost**: Bytes transferred TiDB ↔ TiKV
- **CPU Cost**: Filter evaluations, hash joins
- **Memory Cost**: Sort buffers, hash tables

### Index Selection

**Example Query**:
```sql
SELECT * FROM users WHERE age > 30 AND city = 'NYC';
```

**Available Indexes**:
1. PRIMARY KEY (id)
2. INDEX idx_age (age)
3. INDEX idx_city (city)
4. INDEX idx_age_city (age, city)

**Cost Calculation** [`pkg/planner/core/find_best_task.go:400`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/find_best_task.go#L400):

```go
func (ds *DataSource) findBestTask() task {
    tasks := make([]task, 0)

    // Option 1: Table scan
    tasks = append(tasks, ds.getTableScanTask())

    // Option 2-5: Index scans
    for _, idx := range ds.possibleIndexes {
        tasks = append(tasks, ds.getIndexScanTask(idx))
    }

    // Find minimum cost
    bestTask := tasks[0]
    for _, t := range tasks[1:] {
        if t.cost() < bestTask.cost() {
            bestTask = t
        }
    }

    return bestTask
}
```

**Cost Comparison**:

| Option | Estimated Rows | Cost Calculation |
|--------|----------------|------------------|
| Table Scan | 1,000,000 | 1M × scanCost = 10,000 |
| idx_age | 300,000 | 300K × scanCost = 3,000 |
| idx_city | 100,000 | 100K × scanCost = 1,000 |
| idx_age_city | 10,000 | 10K × scanCost = 100 ← **Best!** |

**Winner**: Composite index `idx_age_city`

**Why?** Covers both conditions → most selective

---

## Statistics: The Optimizer's Eyes

**Without statistics, optimizer is blind!**

### What Statistics Contain

**File**: [`pkg/statistics/handle/handle.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/statistics/handle/handle.go)

```go
type Table struct {
    Columns map[int64]*Column
    Indices map[int64]*Index
    Count   int64  // Total row count
}

type Column struct {
    Histogram  *Histogram        // Value distribution
    CMSketch   *CMSketch         // Cardinality estimator
    TopN       *TopN             // Most frequent values
    NDV        int64             // Number of distinct values
}
```

**Histogram**: Value distribution

```
age column:
[0-20):   100,000 rows
[20-40):  500,000 rows
[40-60):  300,000 rows
[60-80):  100,000 rows
```

**TopN**: Most common values

```
city column:
"NYC":        300,000 rows
"LA":         200,000 rows
"Chicago":    150,000 rows
...
```

**NDV** (Number of Distinct Values):
```
age: ~80 distinct values
city: ~500 distinct values
```

### Selectivity Estimation

**File**: [`pkg/planner/core/stats.go:200`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/stats.go#L200)

**Example**: `WHERE age > 30`

```go
func (ds *DataSource) estimateFilterSelectivity(condition expression.Expression) float64 {
    col := extractColumn(condition)
    hist := ds.stats.Columns[col.ID].Histogram

    // Count rows where age > 30
    count := hist.GreaterRowCount(30)

    // Selectivity = matching rows / total rows
    return float64(count) / float64(ds.stats.Count)
}
```

**Result**: If 70% of users are age > 30, selectivity = 0.7

**Used for**: Estimating result size after filter

---

### Collecting Statistics

**Manual**:
```sql
ANALYZE TABLE users;
```

**Automatic** [`pkg/domain/domain.go:1500`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/domain/domain.go#L1500):

```go
func (do *Domain) startAutoAnalyze() {
    for {
        time.Sleep(analyzeInterval)

        // Find tables needing analysis
        tables := do.statsHandle.NeedAnalyze()

        for _, tbl := range tables {
            // Analyze in background
            go analyzeTable(tbl)
        }
    }
}
```

**Triggered when**:
- Table modified > 20% since last analyze
- New table created
- Manual request

**Cost**: Full table scan (can be expensive!)

---

## Plan Caching

**Problem**: Re-parsing and re-optimizing same query is wasteful

### Prepared Statement Cache

**File**: [`pkg/sessionctx/stmtctx/stmtctx.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessionctx/stmtctx/stmtctx.go)

**Usage**:
```sql
PREPARE stmt FROM 'SELECT * FROM users WHERE id = ?';
EXECUTE stmt USING @user_id;
```

**Cache Key**: Statement ID + schema version

**Cached**: Entire execution plan

**Speedup**: 10x (0.5ms planning → 0.05ms cache lookup)

### Non-Prepared Plan Cache

**File**: [`pkg/planner/core/plan_cache.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/planner/core/plan_cache.go)

**Enabled by**:
```sql
SET SESSION tidb_enable_non_prepared_plan_cache = ON;
```

**Example**:
```sql
SELECT * FROM users WHERE id = 42;
SELECT * FROM users WHERE id = 100;  -- Same plan!
```

**Cache Key**: Normalized SQL + schema version

**Normalization**:
```
SELECT * FROM users WHERE id = 42;
SELECT * FROM users WHERE id = 100;
    ↓
SELECT * FROM users WHERE id = ?;  -- Cache key
```

**Eviction**: LRU (Least Recently Used)

**Configuration**:
```sql
SET SESSION tidb_prepared_plan_cache_size = 100;
```

---

## Common Optimization Pitfalls

### Pitfall 1: Stale Statistics

**Problem**: Optimizer uses outdated statistics

**Symptom**:
```sql
EXPLAIN SELECT * FROM users WHERE created_at > '2025-01-01';
-- Shows: 100 rows (but actually 1M new rows inserted!)
```

**Solution**:
```sql
ANALYZE TABLE users;
```

**Prevention**: Auto-analyze (ensure it's enabled)

### Pitfall 2: Non-Sargable Predicates

**Sargable** = "Search ARGument ABLE" (can use index)

**Bad** (non-sargable):
```sql
WHERE YEAR(created_at) = 2025
WHERE LOWER(email) = 'alice@example.com'
WHERE amount + tax > 1000
```

**Good** (sargable):
```sql
WHERE created_at >= '2025-01-01' AND created_at < '2026-01-01'
WHERE email = 'alice@example.com'  -- If case-insensitive collation
WHERE amount > 1000 - tax
```

**Why?** Functions prevent index usage!

### Pitfall 3: Implicit Type Conversion

**Bad**:
```sql
-- id is INT
SELECT * FROM users WHERE id = '42';  -- String!
```

**What happens**: TiDB converts `id` to string → can't use index

**Good**:
```sql
SELECT * FROM users WHERE id = 42;  -- Correct type
```

### Pitfall 4: Correlated Subqueries

**Bad**:
```sql
SELECT * FROM orders o
WHERE amount > (
    SELECT AVG(amount) FROM orders WHERE customer_id = o.customer_id
);
```

**Problem**: Subquery runs for every row!

**Better**:
```sql
WITH customer_avg AS (
    SELECT customer_id, AVG(amount) AS avg_amount
    FROM orders
    GROUP BY customer_id
)
SELECT o.* FROM orders o
JOIN customer_avg ca ON o.customer_id = ca.customer_id
WHERE o.amount > ca.avg_amount;
```

**Optimizer can sometimes decorrelate automatically** (Rule 3: `decorrelateSolver`)

---

## Tuning the Optimizer

### System Variables

```sql
-- Enable new cost model
SET SESSION tidb_cost_model_version = 2;

-- Disable specific optimizations
SET SESSION tidb_enable_outer_join_reorder = OFF;

-- Control join algorithm preference
SET SESSION tidb_prefer_broadcast_join_by_exchange_data_size = 0;

-- Memory limit for operators
SET SESSION tidb_mem_quota_query = 1073741824;  -- 1GB
```

### Query Hints

**Force specific index**:
```sql
SELECT /*+ USE_INDEX(users, idx_age) */ *
FROM users WHERE age > 30;
```

**Force join order**:
```sql
SELECT /*+ LEADING(t1, t2) */ *
FROM t1 JOIN t2 ON t1.id = t2.id;
```

**Force join algorithm**:
```sql
SELECT /*+ HASH_JOIN(t1, t2) */ *
FROM t1 JOIN t2 ON t1.id = t2.id;
```

**Read from TiFlash**:
```sql
SELECT /*+ READ_FROM_STORAGE(TIFLASH[orders]) */ *
FROM orders WHERE amount > 10000;
```

---

## Monitoring Optimizer Behavior

### EXPLAIN

```sql
EXPLAIN SELECT * FROM users WHERE age > 30;
```

**Output**:
```
+------------------+----------+------+------------------------+
| id               | count    | task | operator info          |
+------------------+----------+------+------------------------+
| TableScan_5      | 700000   | cop  | table:users, range...  |
+------------------+----------+------+------------------------+
```

**Key fields**:
- `count`: Estimated rows
- `task`: Where executed (root=TiDB, cop=TiKV)
- `operator info`: Details

### EXPLAIN ANALYZE

```sql
EXPLAIN ANALYZE SELECT * FROM users WHERE age > 30;
```

**Shows actual vs. estimated**:
```
+------------------+----------+----------+------+
| id               | estRows  | actRows  | task |
+------------------+----------+----------+------+
| TableScan_5      | 700000   | 850000   | cop  |
+------------------+----------+----------+------+
```

**Interpretation**: Estimation was off by 21%

### TRACE

**See optimization steps**:
```sql
TRACE SELECT * FROM users WHERE age > 30;
```

**Output**: JSON showing each optimization rule applied

---

## Key Takeaways

1. **Two-Phase Optimization**: Rule-based (logical) → Cost-based (physical)

2. **15+ Optimization Rules**: Applied in sequence, each transforming the plan

3. **Statistics Drive Decisions**: Without accurate stats, optimizer guesses poorly

4. **Plan Caching**: 10x speedup for repeated queries

5. **Join Reordering**: DP for ≤6 tables, greedy for ≥7 tables

6. **Predicate Push-Down**: Critical for distributed performance

7. **Index Selection**: Based on selectivity, not just presence of index

---

## Best Practices

1. **Keep statistics fresh**: Run `ANALYZE TABLE` regularly
2. **Write sargable predicates**: Avoid functions on indexed columns
3. **Use appropriate data types**: Prevent implicit conversions
4. **Monitor with EXPLAIN ANALYZE**: Catch estimation errors
5. **Use hints sparingly**: Let optimizer do its job, override only when necessary

---

## Next Steps

**Continue Reading**:
- **[Part 4: Distributed Transactions](./04-distributed-transactions.md)** - 2PC and MVCC internals

**Experiment**:
```sql
-- Compare plans
EXPLAIN SELECT ...;
EXPLAIN ANALYZE SELECT ...;

-- Force different strategies
SELECT /*+ USE_INDEX(...) */ ...;
SELECT /*+ HASH_JOIN(...) */ ...;
```

**Explore Code**:
- Optimizer entry: `pkg/planner/core/optimizer.go`
- Rules: `pkg/planner/core/rule_*.go`
- Cost model: `pkg/planner/core/task.go`

---

**Part 3 of 7 - Next**: [Distributed Transactions →](./04-distributed-transactions.md)
