# Execution Engine Internals: Operators, Chunks, and Memory Management

**Blog Series**: TiDB Deep Dive (Part 5 of 7)
**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
**Read Time**: 18 minutes

---

## What You'll Learn

- How executors implement the iterator pattern
- Chunk-based batch processing for efficiency
- Memory tracking and spill-to-disk mechanisms
- Join algorithm implementations (hash, merge, index)
- Aggregation strategies (hash vs. stream)
- Performance optimization techniques

---

## The Executor Model

**File**: [`pkg/executor/internal/exec/executor.go:41`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/internal/exec/executor.go#L41)

```go
type Executor interface {
    // Initialize resources
    Open(context.Context) error

    // Fetch next batch of rows
    Next(context.Context, *chunk.Chunk) error

    // Clean up resources
    Close() error

    // Schema information
    Schema() *expression.Schema
}
```

**Iterator Pattern** (Volcano model with batching):
```
executor.Open()
for {
    chunk := NewChunk()
    err := executor.Next(ctx, chunk)
    if chunk.NumRows() == 0 {
        break
    }
    // Process chunk...
}
executor.Close()
```

---

## Chunk: Columnar Batching

**File**: [`pkg/util/chunk/chunk.go:42`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/util/chunk/chunk.go#L42)

### Structure

```go
type Chunk struct {
    columns     []*Column
    capacity    int        // Max rows (default 1024)
    numVirtualRows int     // Current rows
}

type Column struct {
    length     int
    nullBitmap []byte     // 1 bit per row
    offsets    []int64    // For variable-length data
    data       []byte     // Actual data
    elemBuf    []byte     // Temporary buffer
}
```

### Why Columnar?

**Row-based** (traditional):
```
Row 1: [id=1, name="Alice", age=30]
Row 2: [id=2, name="Bob", age=25]
```

**Column-based** (TiDB chunks):
```
Column 0 (id):   [1, 2]
Column 1 (name): ["Alice", "Bob"]
Column 2 (age):  [30, 25]
```

**Advantages**:
- ✅ Cache-friendly access (CPU cache lines)
- ✅ SIMD vectorization possible
- ✅ Better compression
- ✅ Efficient column pruning

### Memory Layout

```
Chunk for SELECT id, name FROM users LIMIT 3:

Column 0 (INT id):
nullBitmap: [0,0,0]  (no NULLs)
data: [1, 2, 3]  (8 bytes each = 24 bytes total)

Column 1 (VARCHAR name):
nullBitmap: [0,0,0]
offsets: [0, 5, 8, 12]  (start positions)
data: "AliceBobEve"  (12 bytes)
```

---

## Scan Executors

### TableReaderExecutor

**File**: [`pkg/executor/table_reader.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/table_reader.go)

**Purpose**: Distributed table scan via TiKV coprocessor

```go
func (e *TableReaderExecutor) Open(ctx context.Context) error {
    // Build key ranges for scan
    ranges := e.buildKeyRanges()

    // Create DistSQL request
    req := &kv.Request{
        KeyRanges: ranges,
        KeepOrder: e.keepOrder,
        Desc:      e.desc,
    }

    // Send to TiKV
    e.result, err = distsql.Select(ctx, e.ctx, req)
    return err
}

func (e *TableReaderExecutor) Next(ctx context.Context, chk *chunk.Chunk) error {
    // Stream results from TiKV
    return e.result.Next(ctx, chk)
}
```

**Coprocessor push-down**:
```sql
SELECT * FROM users WHERE age > 30;
```

**Pushed to TiKV**:
- Filter: `age > 30`
- Projection: All columns
- Execution: Parallel across all regions

**Network saved**: If 90% filtered, only 10% transferred

### PointGetExecutor

**File**: [`pkg/executor/point_get.go:100`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/point_get.go#L100)

**Purpose**: Single-row lookup by primary key

```go
func (e *PointGetExecutor) Next(ctx context.Context, chk *chunk.Chunk) error {
    if e.done {
        return nil
    }

    // Build key: t{tableID}_r{rowID}
    key := tablecodec.EncodeRowKey(e.tblInfo.ID, e.handle)

    // Get from snapshot
    val, err := e.snapshot.Get(ctx, key)

    // Decode row
    row, err := tablecodec.DecodeRow(val, e.columns)

    // Append to chunk
    chk.AppendRow(row)

    e.done = true
    return nil
}
```

**Optimized path**: 1 network round-trip to TiKV

---

## Join Executors

### Hash Join

**File**: [`pkg/executor/join/hash_join_v2.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/join/hash_join_v2.go)

**Algorithm**:
```
1. Build phase: Scan inner table, build hash table
2. Probe phase: Scan outer table, probe hash table
```

**Implementation**:
```go
type HashJoinV2Exec struct {
    baseExecutor
    buildWorkers  []*buildWorker    // Parallel build
    probeWorkers  []*probeWorker    // Parallel probe
    hashTable     *rowHashMap       // Join hash table
}

func (e *HashJoinV2Exec) Open(ctx context.Context) error {
    // Start build workers
    for i := range e.buildWorkers {
        go e.buildWorkers[i].run(ctx)
    }

    // Wait for build to complete
    e.waitBuildComplete()

    // Start probe workers
    for i := range e.probeWorkers {
        go e.probeWorkers[i].run(ctx)
    }

    return nil
}
```

**Build phase**:
```go
func (w *buildWorker) run(ctx context.Context) {
    for {
        // Read chunk from inner table
        chk := w.fetchInnerChunk(ctx)
        if chk.NumRows() == 0 {
            break
        }

        // Insert into hash table
        for rowIdx := 0; rowIdx < chk.NumRows(); rowIdx++ {
            row := chk.GetRow(rowIdx)
            key := w.computeJoinKey(row)
            w.hashTable.Put(key, row)
        }
    }
}
```

**Probe phase**:
```go
func (w *probeWorker) run(ctx context.Context) {
    for {
        // Read chunk from outer table
        chk := w.fetchOuterChunk(ctx)

        // Probe hash table
        for rowIdx := 0; rowIdx < chk.NumRows(); rowIdx++ {
            outerRow := chk.GetRow(rowIdx)
            key := w.computeJoinKey(outerRow)

            // Lookup matching rows
            innerRows := w.hashTable.Get(key)

            // Emit joined rows
            for _, innerRow := range innerRows {
                w.emitJoinedRow(outerRow, innerRow)
            }
        }
    }
}
```

**Parallelism**: Configurable workers (default 4-8)

**Spill-to-disk**: If hash table exceeds memory limit

**File**: [`pkg/executor/join/hash_table_v2.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/join/hash_table_v2.go)

```go
func (t *rowHashMap) checkSpillNeed() bool {
    memUsage := t.memTracker.BytesConsumed()
    if memUsage > t.spillThreshold {
        // Spill partition to disk
        return t.spillPartition()
    }
    return false
}
```

### Index Join

**File**: [`pkg/executor/index_lookup_join.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/index_lookup_join.go)

**Algorithm**: Nested loop with index lookup

```
For each row in outer table:
    Use join key to lookup inner table via index
    Emit matched rows
```

**Best for**: Small outer table, large inner table with index

**Example**:
```sql
SELECT * FROM orders o
JOIN customers c ON o.customer_id = c.id
WHERE o.order_date > '2025-01-01';
```

**If** `orders` filtered to 100 rows, `customers.id` has index:
```
For each of 100 orders:
    Index lookup on customers(id)  -- Single row fetch
Total: 100 point gets (efficient!)
```

**vs. Hash Join**: Would scan entire customers table

---

## Aggregation Executors

### Hash Aggregation

**File**: [`pkg/executor/aggregate.go:250`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/aggregate.go#L250)

**Use case**: Unsorted input

```sql
SELECT region, COUNT(*), SUM(amount)
FROM orders
GROUP BY region;
```

**Algorithm**:
```go
type HashAggExec struct {
    baseExecutor
    groupMap  map[string]*AggFuncGroup  // Group key → Aggregators
    partialWorkers []*AggWorker         // Parallel aggregation
}

func (e *HashAggExec) Next(ctx context.Context, chk *chunk.Chunk) error {
    // If first call, compute aggregations
    if !e.prepared {
        e.prepare(ctx)
    }

    // Emit results
    for groupKey, agg := range e.groupMap {
        row := e.buildResultRow(groupKey, agg)
        chk.AppendRow(row)
        if chk.IsFull() {
            return nil
        }
    }

    return nil
}

func (e *HashAggExec) prepare(ctx context.Context) {
    // Read all input
    for {
        inputChk := e.newChunk()
        err := e.children[0].Next(ctx, inputChk)
        if inputChk.NumRows() == 0 {
            break
        }

        // Update aggregations
        for rowIdx := 0; rowIdx < inputChk.NumRows(); rowIdx++ {
            row := inputChk.GetRow(rowIdx)
            groupKey := e.computeGroupKey(row)

            // Get or create aggregator group
            agg := e.getAggGroup(groupKey)

            // Update COUNT, SUM, etc.
            agg.Update(row)
        }
    }

    e.prepared = true
}
```

**Partial aggregation** (on TiKV):
```
TiKV-1: Partial agg for its data
TiKV-2: Partial agg for its data
TiKV-3: Partial agg for its data
    ↓
TiDB: Final aggregation (merge partial results)
```

**Memory management**:
```go
func (e *HashAggExec) checkSpill() {
    if e.memTracker.BytesConsumed() > e.memLimit {
        e.spillToDisk()
    }
}
```

### Stream Aggregation

**File**: [`pkg/executor/aggregate.go:450`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/aggregate.go#L450)

**Use case**: Sorted input

**Requirement**: Input must be sorted by GROUP BY columns

```sql
SELECT region, COUNT(*)
FROM orders
ORDER BY region  -- Sorted!
GROUP BY region;
```

**Algorithm**:
```go
func (e *StreamAggExec) Next(ctx context.Context, chk *chunk.Chunk) error {
    for {
        // Read next row
        row, err := e.readNextRow(ctx)
        if err != nil {
            break
        }

        groupKey := e.computeGroupKey(row)

        if groupKey != e.currentGroupKey {
            // Group changed, emit current group
            resultRow := e.buildResultRow(e.currentGroupKey, e.aggFuncs)
            chk.AppendRow(resultRow)

            // Start new group
            e.currentGroupKey = groupKey
            e.resetAggFuncs()

            if chk.IsFull() {
                return nil
            }
        }

        // Update aggregations for current group
        e.updateAggFuncs(row)
    }

    // Emit last group
    if e.currentGroupKey != nil {
        resultRow := e.buildResultRow(e.currentGroupKey, e.aggFuncs)
        chk.AppendRow(resultRow)
    }

    return nil
}
```

**Advantages**:
- ✅ Streaming (no buffer all input)
- ✅ Lower memory usage
- ✅ Can start emitting results early

**Disadvantage**:
- ❌ Requires sorted input (extra cost if not already sorted)

**Optimizer chooses**: Stream agg if input is sorted, hash agg otherwise

---

## Memory Management

### Memory Tracking

**File**: [`pkg/util/memory/tracker.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/util/memory/tracker.go)

```go
type Tracker struct {
    label         string
    bytesConsumed int64
    bytesLimit    int64
    parent        *Tracker
    children      []*Tracker
}

func (t *Tracker) Consume(bytes int64) {
    atomic.AddInt64(&t.bytesConsumed, bytes)

    // Check limit
    if t.bytesConsumed > t.bytesLimit {
        panic("memory quota exceeded")
    }

    // Propagate to parent
    if t.parent != nil {
        t.parent.Consume(bytes)
    }
}
```

**Hierarchy**:
```
Global Tracker (server-level)
    ├── Session Tracker (per connection)
    │       ├── Query Tracker
    │       │       ├── Hash Join Tracker
    │       │       ├── Hash Agg Tracker
    │       │       └── Sort Tracker
    │       └── Transaction Tracker
    └── ...
```

**Configuration**:
```sql
-- Per-query limit
SET SESSION tidb_mem_quota_query = 1073741824;  -- 1GB

-- Server-level limit
SET GLOBAL tidb_server_memory_limit = "75%";
```

### Spill-to-Disk

**File**: [`pkg/executor/sortexec/sort.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/sortexec/sort.go)

**Triggered when**:
```go
func (s *SortExec) spillCheck() bool {
    memUsage := s.memTracker.BytesConsumed()
    if memUsage > s.memLimit {
        return s.spillToDisk()
    }
    return false
}

func (s *SortExec) spillToDisk() error {
    // 1. Sort in-memory chunks
    sortChunks(s.chunks)

    // 2. Write to temporary file
    file, err := ioutil.TempFile(s.tempDir, "sort_spill_")

    for _, chk := range s.chunks {
        chk.WriteTo(file)
    }

    s.spilledFiles = append(s.spilledFiles, file)

    // 3. Clear memory
    s.chunks = nil
    s.memTracker.Consume(-memUsage)

    return nil
}
```

**External sort**:
```
1. Spill sorted runs to disk (multiple files)
2. Merge sorted runs (k-way merge)
3. Emit final sorted output
```

**Temporary storage**: Configured via `tmp-storage-path`

---

## Sort Executor

**File**: [`pkg/executor/sortexec/sort.go:100`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/executor/sortexec/sort.go#L100)

**Algorithm**: Quicksort + external merge sort

```go
func (s *SortExec) Next(ctx context.Context, chk *chunk.Chunk) error {
    if !s.fetched {
        // Fetch all input
        s.fetchAll(ctx)

        // Sort (in memory if fits, otherwise spill)
        s.sort(ctx)

        s.fetched = true
    }

    // Return sorted rows
    return s.emitSorted(chk)
}

func (s *SortExec) fetchAll(ctx context.Context) error {
    for {
        inputChk := s.newChunk()
        err := s.children[0].Next(ctx, inputChk)
        if inputChk.NumRows() == 0 {
            break
        }

        s.chunks = append(s.chunks, inputChk)

        // Check if need to spill
        if s.spillCheck() {
            s.spillToDisk()
        }
    }
    return nil
}
```

**Parallel sort**:
```go
func (s *SortExec) parallelSort(chunks []*chunk.Chunk) {
    // Divide chunks among workers
    numWorkers := runtime.NumCPU()
    chunkGroups := divideChunks(chunks, numWorkers)

    // Sort each group in parallel
    var wg sync.WaitGroup
    for i, group := range chunkGroups {
        wg.Add(1)
        go func(group []*chunk.Chunk) {
            defer wg.Done()
            sortChunks(group)
        }(group)
    }
    wg.Wait()

    // Merge sorted groups
    merged := mergeSortedGroups(chunkGroups)
    return merged
}
```

---

## Key Takeaways

1. **Iterator pattern** with chunk batching (1024 rows default)
2. **Columnar layout** for cache efficiency and vectorization
3. **Memory tracking** prevents OOMs via hierarchical limits
4. **Spill-to-disk** enables large operations beyond memory
5. **Parallel execution** (hash join, agg, sort) for multi-core utilization
6. **Coprocessor push-down** critical for distributed performance

---

## Performance Tips

1. **Use indexes for joins**: Index join can be 100x faster than hash join
2. **Keep intermediate results small**: Filters before joins
3. **Monitor memory usage**:
   ```sql
   SELECT * FROM information_schema.processlist
   WHERE MEM > 1073741824;  -- Queries using >1GB
   ```
4. **Tune memory limits**:
   ```sql
   SET SESSION tidb_mem_quota_query = 2147483648;  -- 2GB
   ```
5. **Enable parallel execution**:
   ```sql
   SET SESSION tidb_executor_concurrency = 8;
   ```

---

## Next Steps

**Continue Reading**:
- **[Part 6: Schema Changes at Scale](./06-online-ddl.md)**

**Explore Code**:
- Executors: `pkg/executor/`
- Chunks: `pkg/util/chunk/`
- Memory: `pkg/util/memory/`

---

**Part 5 of 7 - Next**: [Online DDL →](./06-online-ddl.md)
