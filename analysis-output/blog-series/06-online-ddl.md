# Schema Changes at Scale: Online DDL Deep Dive

**Blog Series**: TiDB Deep Dive (Part 6 of 7)
**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
**Read Time**: 14 minutes

---

## What You'll Learn

- How TiDB performs non-blocking schema changes
- The state transition protocol (based on Google F1)
- Owner election and distributed coordination
- Index backfilling with minimal impact
- Reorg workers and progress tracking
- Limitations and edge cases

---

## The Online DDL Challenge

**Problem**: Traditional databases lock tables during ALTER:
```sql
ALTER TABLE users ADD COLUMN phone VARCHAR(20);
-- Table locked for minutes/hours!
```

**TiDB's Solution**: Online schema change
- ✅ Reads and writes continue during DDL
- ✅ No table locking
- ✅ Automatic coordination across cluster

**Based on**: Google F1's Online Schema Change algorithm

---

## State Transition Protocol

**File**: [`pkg/ddl/ddl.go:800`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/ddl.go#L800)

### Column Addition States

```
None → Delete Only → Write Only → Write Reorg → Public
```

**State meanings**:
- **None**: Column doesn't exist
- **Delete Only**: Can delete, can't read/write
- **Write Only**: Can write, can't read
- **Write Reorg**: Backfilling old rows
- **Public**: Fully available

### Why Multiple States?

**Problem**: Distributed servers see schema at different times

**Solution**: Gradual visibility

**Example**:
```
Server A sees: users (id, name)  -- Old schema
Server B sees: users (id, name, phone)  -- New schema

If column immediately public:
- Server A inserts: (1, "Alice")  -- Missing phone!
- Server B reads: (1, "Alice", NULL)  -- OK
- But Server B writes: (2, "Bob", "555-1234")
- Server A reads: Error! Unknown column 'phone'
```

**With states**:
```
Phase 1 (Delete Only):
- All servers can ignore phone column
- Safe state for transition

Phase 2 (Write Only):
- All servers write phone (if present)
- But don't return it in SELECTs
- Old data gradually gets phone=NULL

Phase 3 (Write Reorg):
- Backfill phone for old rows
- Still not visible in SELECTs

Phase 4 (Public):
- All servers see phone column
- Fully usable
```

---

## DDL Owner Election

**File**: [`pkg/ddl/owner/owner.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/owner/owner.go)

**Problem**: Which TiDB server executes DDL?

**Solution**: Leader election via etcd

```go
type ownerManager struct {
    etcdCli *clientv3.Client
    id      string  // This server's ID
    key     string  // "/tidb/ddl/owner"
}

func (m *ownerManager) campaign Ctx() error {
    session, err := concurrency.NewSession(m.etcdCli)

    election := concurrency.NewElection(session, m.key)

    // Try to become leader
    err = election.Campaign(ctx, m.id)

    // If successful, we're the owner!
    return err
}
```

**Only the owner** executes DDL jobs

**Other servers**: Wait for schema version updates

---

## DDL Job Queue

**File**: [`pkg/ddl/ddl.go:600`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/ddl.go#L600)

**Storage**: Persisted in TiKV (system table: `mysql.tidb_ddl_job`)

```go
type Job struct {
    ID          int64
    Type        ActionType  // AddColumn, DropIndex, etc.
    SchemaID    int64
    TableID     int64
    State       JobState    // Queued, Running, Done, Failed
    Args        []interface{}  // Job-specific parameters
}
```

**Lifecycle**:
```
1. Client submits: ALTER TABLE users ADD COLUMN phone VARCHAR(20);
2. DDL module creates Job, stores in queue
3. Owner picks up job from queue
4. Owner executes job (state transitions)
5. Owner marks job as Done
6. All servers reload schema
```

---

## Schema Version and Lease

**File**: [`pkg/infoschema/infoschema.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/infoschema/infoschema.go)

**Schema Version**: Incremented on every DDL

```
V100: users (id, name)
V101: users (id, name, phone)  -- After ADD COLUMN
```

**Schema Lease**: Time window for synchronization

**Default**: 45 seconds

**Lease guarantees**:
- All servers use schemas within 2 versions of latest
- If server has V99 and latest is V101, server must reload

**Reload mechanism**:
```go
func (do *Domain) loadSchemaInLoop(ctx context.Context) {
    ticker := time.NewTicker(leaseTime / 2)  // Every 22.5s

    for {
        select {
        case <-ticker.C:
            latestVersion := do.getLatestSchemaVersion()

            if latestVersion > do.infoSchema.SchemaVersion {
                do.infoSchema = do.loadInfoSchema(latestVersion)
            }
        }
    }
}
```

---

## Add Column Implementation

**File**: [`pkg/ddl/column.go:200`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/column.go#L200)

### Phase 1: Delete Only

```go
func (w *worker) onAddColumn(d *ddlCtx, t *meta.Meta, job *Job) error {
    // 1. Get table metadata
    tblInfo, err := getTableInfo(t, job)

    // 2. Add column in Delete Only state
    col := &model.ColumnInfo{
        Name:  job.Args[0].(string),
        State: model.StateDeleteOnly,
    }
    tblInfo.Columns = append(tblInfo.Columns, col)

    // 3. Update metadata, increment schema version
    err = t.UpdateTable(job.SchemaID, tblInfo)

    // 4. Wait for lease (ensure all servers see change)
    time.Sleep(2 * lease)

    return nil
}
```

### Phase 2: Write Only

```go
func (w *worker) onAddColumn_WriteOnly(d *ddlCtx, t *meta.Meta, job *Job) error {
    // Transition: Delete Only → Write Only
    col.State = model.StateWriteOnly

    err = t.UpdateTable(job.SchemaID, tblInfo)

    // Wait for lease
    time.Sleep(2 * lease)

    return nil
}
```

**At this point**: All writes include new column (as NULL if not specified)

### Phase 3: Write Reorganization

```go
func (w *worker) onAddColumn_WriteReorg(d *ddlCtx, t *meta.Meta, job *Job) error {
    // Transition: Write Only → Write Reorganization
    col.State = model.StateWriteReorganization

    // Start backfilling existing rows
    reorgInfo := &reorgInfo{
        Job:       job,
        d:         d,
        StartKey:  tblInfo.GetStartKey(),
        EndKey:    tblInfo.GetEndKey(),
    }

    err = w.runReorgJob(reorgInfo, func() error {
        return w.backfillColumn(reorgInfo)
    })

    return nil
}
```

**Backfilling**:
```
For each row in table:
    If column value is missing:
        Write default value or NULL
```

### Phase 4: Public

```go
func (w *worker) onAddColumn_Public(d *ddlCtx, t *meta.Meta, job *Job) error {
    // Transition: Write Reorganization → Public
    col.State = model.StatePublic

    err = t.UpdateTable(job.SchemaID, tblInfo)

    // DDL job complete!
    job.State = model.JobStateDone

    return nil
}
```

**Column now fully available!**

---

## Index Backfilling

**File**: [`pkg/ddl/backfilling.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/backfilling.go)

### Reorg Workers

```go
type reorgWorker struct {
    id         int
    reorgInfo  *reorgInfo
    batchSize  int  // Rows per batch (default 2048)
}

func (w *reorgWorker) run(ctx context.Context) {
    for {
        // Get next batch of rows
        startKey := w.reorgInfo.StartKey
        endKey := w.reorgInfo.EndKey

        rows, nextKey, err := w.fetchRows(startKey, endKey, w.batchSize)
        if len(rows) == 0 {
            break  // Done
        }

        // Build index entries for batch
        err = w.buildIndexForRows(rows)

        // Update progress
        w.reorgInfo.UpdatedHandle = nextKey
    }
}
```

**Parallelism**: Multiple workers (default 4)

**Rate limiting**: Avoid overloading cluster

```go
const reorgWorkerWaitTimeout = 30 * time.Millisecond

func (w *reorgWorker) buildIndexForRows(rows []kv.Row) error {
    for _, row := range rows {
        // Extract index key
        indexKey := buildIndexKey(row)

        // Write to TiKV
        err := w.addIndexRecord(indexKey, row.Handle)

        // Throttle
        time.Sleep(reorgWorkerWaitTimeout / len(rows))
    }
}
```

### Progress Tracking

**File**: [`pkg/ddl/reorg.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/reorg.go)

```sql
-- Check progress
SELECT 
    JOB_ID,
    DB_NAME,
    TABLE_NAME,
    JOB_TYPE,
    SCHEMA_STATE,
    ROW_COUNT,
    PROGRESS
FROM information_schema.DDL_JOBS
WHERE STATE = 'running';
```

**Output**:
```
+--------+---------+------------+----------+--------------+-----------+----------+
| JOB_ID | DB_NAME | TABLE_NAME | JOB_TYPE | SCHEMA_STATE | ROW_COUNT | PROGRESS |
+--------+---------+------------+----------+--------------+-----------+----------+
|    123 | test    | users      | add index| write reorg  |  50000000 | 45.2%    |
+--------+---------+------------+----------+--------------+-----------+----------+
```

---

## Fast Index Creation (Lightning Mode)

**File**: [`pkg/ddl/ingest/ingest.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/ddl/ingest/ingest.go)

**Problem**: Backfilling via SQL is slow for large tables

**Solution**: Direct SST file generation

```go
func (b *BackendContext) CreateIndex() error {
    // 1. Scan table, build index entries
    indexData := b.scanAndBuildIndex()

    // 2. Sort index entries
    sort.Slice(indexData, func(i, j int) bool {
        return bytes.Compare(indexData[i].Key, indexData[j].Key) < 0
    })

    // 3. Generate SST file
    sstFile, err := b.buildSSTFile(indexData)

    // 4. Ingest into TiKV (bypasses Raft)
    err = b.ingestSST(sstFile)

    return nil
}
```

**Speedup**: 10-50x faster than traditional backfilling

**Enabled**:
```sql
SET @@tidb_ddl_enable_fast_reorg = ON;
```

---

## DDL Limitations

### Non-Online Operations

**These still require table rebuilds**:
```sql
-- Change column type (incompatible)
ALTER TABLE users MODIFY COLUMN age BIGINT;

-- Change charset
ALTER TABLE users CONVERT TO CHARACTER SET utf8mb4;
```

**Workaround**: Create new table, copy data, swap

### Blocked by Long Transactions

**Problem**: DDL waits for old transactions

**Example**:
```sql
-- Transaction T1 started with schema V100
BEGIN;
SELECT * FROM users;  -- Sees V100

-- DDL submitted
ALTER TABLE users ADD COLUMN phone VARCHAR(20);  -- Creates V101

-- DDL waits for T1 to commit!
-- T1 still using V100, can't have V101 public yet
```

**Solution**: Keep transactions short

### Metadata Lock Timeout

**If DDL can't acquire metadata lock**:
```sql
SET GLOBAL lock_wait_timeout = 31536000;  -- 1 year
```

---

## Monitoring DDL

### Active DDL Jobs

```sql
SELECT * FROM information_schema.DDL_JOBS
WHERE STATE IN ('queued', 'running')
ORDER BY JOB_ID;
```

### DDL History

```sql
SELECT * FROM information_schema.DDL_JOBS
WHERE STATE = 'done'
ORDER BY END_TIME DESC
LIMIT 10;
```

### Owner Status

```sql
ADMIN SHOW DDL;
```

**Output**:
```
+----------------+--------------+
| SCHEMA_VER     | OWNER        |
+----------------+--------------+
| 101            | tidb-1:4000  |
+----------------+--------------+
```

---

## Best Practices

1. **Run DDL during low-traffic periods**: Reduces impact

2. **Monitor progress**: Use `information_schema.DDL_JOBS`

3. **Keep transactions short**: Long transactions block DDL

4. **Test on replica first**: Verify DDL behavior

5. **Use fast reorg for large tables**:
   ```sql
   SET @@tidb_ddl_enable_fast_reorg = ON;
   ```

6. **Batch column additions**: One ALTER for multiple columns
   ```sql
   ALTER TABLE users
     ADD COLUMN phone VARCHAR(20),
     ADD COLUMN address TEXT;
   ```

---

## Troubleshooting

### DDL Stuck

**Check**:
```sql
-- Long-running transactions?
SELECT * FROM information_schema.PROCESSLIST
WHERE TIME > 600;  -- >10 minutes

-- DDL owner alive?
ADMIN SHOW DDL;
```

**Fix**:
```sql
-- Kill blocking transaction
KILL <connection_id>;

-- Re-elect DDL owner (if owner crashed)
-- Automatic, wait ~45 seconds
```

### DDL Failed

**Check**:
```sql
SELECT JOB_ID, ERROR FROM information_schema.DDL_JOBS
WHERE STATE = 'cancelled'
ORDER BY END_TIME DESC
LIMIT 1;
```

**Common causes**:
- Incompatible type change
- Duplicate key during index creation
- Out of disk space

---

## Key Takeaways

1. **Online schema change** based on Google F1 algorithm
2. **State transitions** ensure distributed consistency
3. **Owner election** via etcd for coordination
4. **Backfilling** uses reorg workers with rate limiting
5. **Fast reorg** mode for large tables (Lightning SST ingest)
6. **Schema lease** ensures all servers within 2 versions

---

## Next Steps

**Continue Reading**:
- **[Part 7: Production-Grade Observability](./07-observability.md)**

**Explore Code**:
- DDL entry: `pkg/ddl/ddl.go`
- Owner election: `pkg/ddl/owner/`
- Backfilling: `pkg/ddl/backfilling.go`
- Fast reorg: `pkg/ddl/ingest/`

---

**Part 6 of 7 - Next**: [Observability →](./07-observability.md)
