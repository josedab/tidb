# Distributed Transactions: 2PC, MVCC, and Conflict Resolution

**Blog Series**: TiDB Deep Dive (Part 4 of 7)
**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
**Read Time**: 15 minutes

---

## What You'll Learn

- How TiDB achieves ACID across a distributed cluster
- The Percolator two-phase commit protocol
- MVCC (Multi-Version Concurrency Control) in TiKV
- Optimistic vs. pessimistic transaction modes
- Write conflict detection and resolution strategies
- Lock management and garbage collection

---

## The Distributed Transaction Challenge

**Single-node database**: Easy! Locks in memory, WAL on disk, atomic commit.

**Distributed database**: Hard!
- Data spread across 100s of nodes
- Network failures, partial failures
- Concurrent transactions across multiple servers

**TiDB's Solution**: Percolator-based 2PC + MVCC

---

## Timestamp Oracle (TSO)

Every transaction needs globally unique timestamps.

**Source**: PD (Placement Driver)

**File**: Integration with [`tikv/pd/client`](https://github.com/tikv/pd)

```go
// Get start timestamp for new transaction
func (s *session) Begin() error {
    ts, err := s.store.GetOracle().GetTimestamp(ctx)
    s.txnCtx.StartTS = ts
    return nil
}
```

**TSO Properties**:
- Globally unique across cluster
- Monotonically increasing
- Format: 64-bit (physical time + logical counter)

**Example**:
```
Transaction T1: start_ts = 100
Transaction T2: start_ts = 101
```

**Critical**: All reads in T1 see snapshot at timestamp 100

---

## Two-Phase Commit (2PC)

**File**: [`pkg/sessiontxn/`](https://github.com/pingcap/tidb/tree/bd6aa865/pkg/sessiontxn)

### Phase 1: Prewrite

**Goal**: Lock all keys and write intents

```go
func (c *twoPhaseCommitter) prewriteKeys(keys [][]byte) error {
    // 1. Choose primary key (first key)
    primary := keys[0]

    // 2. Prewrite primary
    err := c.prewriteSingleKey(primary, true)

    // 3. Prewrite secondaries
    for _, key := range keys[1:] {
        err := c.prewriteSingleKey(key, false)
    }

    return nil
}
```

**What prewrite does**:
```
1. Check for conflicts (no writes after start_ts)
2. Write lock record
3. Write data intent (uncommitted)
```

**Lock Structure**:
```
Lock Key: l{key}
Lock Value: {
    start_ts: 100,
    primary: {primary_key},
    ttl: 3000  // milliseconds
}
```

### Phase 2: Commit

**Goal**: Make transaction durable

```go
func (c *twoPhaseCommitter) commitKeys(keys [][]byte) error {
    // 1. Get commit timestamp
    commitTS, err := c.store.GetOracle().GetTimestamp(ctx)

    // 2. Commit primary key (critical point!)
    err := c.commitSingleKey(c.primary, commitTS)

    // 3. Async: Commit secondaries
    go func() {
        for _, key := range c.secondaries {
            c.commitSingleKey(key, commitTS)
        }
    }()

    return nil
}
```

**Commit writes**:
```
Write Key: w{key}_{commit_ts}
Write Value: start_ts
```

**After commit**:
```
Key state:
- Lock removed
- Write record at commit_ts
- Data visible to transactions with start_ts >= commit_ts
```

---

## MVCC (Multi-Version Concurrency Control)

**File**: Implemented in TiKV (RocksDB storage)

### Version Storage

```
Key: user_42_balance

Versions:
w_user_42_balance_105: start_ts=100, value=500  (committed at 105)
w_user_42_balance_090: start_ts=085, value=400  (committed at 090)
w_user_42_balance_070: start_ts=065, value=300  (committed at 070)
```

**Read at timestamp 95**:
- Skip version 105 (too new)
- Read version 090 (most recent ≤ 95)
- Result: balance = 400

### Garbage Collection

**File**: [`pkg/domain/domain.go:1600`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/domain/domain.go#L1600)

```go
func (do *Domain) startGCWorker() {
    for {
        time.Sleep(gcInterval)

        // Calculate safe point (oldest active transaction)
        safePoint := do.calculateGCSafePoint()

        // Delete versions older than safe point
        do.gcWorker.RunGC(safePoint)
    }
}
```

**Safe point**: Oldest timestamp any transaction might read

**Example**:
- Oldest active transaction: start_ts = 200
- GC deletes versions with commit_ts < 200

---

## Optimistic vs. Pessimistic Modes

### Optimistic Transactions (Default)

**File**: [`pkg/sessiontxn/isolation/optimistic.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessiontxn/isolation/optimistic.go)

**Flow**:
```
BEGIN
UPDATE users SET balance = 500 WHERE id = 1;  -- No locks!
COMMIT  -- Conflict check happens here
```

**Advantages**:
- Low latency for low-contention workloads
- No deadlocks

**Disadvantages**:
- Write conflicts detected late
- Must retry on conflict

**Code**:
```go
func (t *OptimisticTxn) Commit(ctx context.Context) error {
    // Prewrite (checks for conflicts)
    err := t.prewrite()
    if err == ErrWriteConflict {
        return err  // Client must retry
    }

    // Commit
    return t.commit()
}
```

### Pessimistic Transactions

**File**: [`pkg/sessiontxn/isolation/pessimistic.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessiontxn/isolation/pessimistic.go)

**Flow**:
```
BEGIN PESSIMISTIC;
UPDATE users SET balance = 500 WHERE id = 1;  -- Acquires lock immediately!
-- (block if another transaction holds lock)
COMMIT  -- No conflict possible
```

**Advantages**:
- No surprises at commit time
- Better for high-contention workloads

**Disadvantages**:
- Higher latency (lock acquisition)
- Possible deadlocks

**Code**:
```go
func (t *PessimisticTxn) LockKeys(ctx context.Context, keys []kv.Key) error {
    req := &kvrpcpb.PessimisticLockRequest{
        Mutations: buildMutations(keys),
        StartVersion: t.startTS,
        ForUpdateTS: t.forUpdateTS,
    }

    // Send to TiKV, blocks until lock acquired
    return t.store.SendReq(ctx, req)
}
```

---

## Conflict Detection and Resolution

### Write-Write Conflicts

**Scenario**:
```
T1 (start_ts=100): UPDATE users SET balance = 500 WHERE id = 1
T2 (start_ts=105): UPDATE users SET balance = 600 WHERE id = 1

Both commit simultaneously
```

**Detection** (in prewrite):
```go
func (t *twoPhaseCommitter) checkForConflict(key []byte) error {
    // Read latest write
    latestWrite := readLatestWrite(key)

    if latestWrite.commit_ts >= t.startTS {
        return ErrWriteConflict  // Another transaction wrote after we started
    }

    return nil
}
```

**Resolution**: First to commit wins, second gets error

```
T1 prewrite at 110: ✅ Success (no conflict)
T1 commit at 112: ✅ Committed

T2 prewrite at 111: ❌ Error: WriteConflict (T1 committed at 112 > T2 start_ts 105)
```

**Client retry**:
```go
for retries := 0; retries < maxRetries; retries++ {
    err := session.Commit()
    if err != ErrWriteConflict {
        return err
    }

    // Exponential backoff
    time.Sleep(backoff(retries))

    // Restart transaction with new timestamp
    session.Begin()
    // Re-execute statements...
}
```

---

## Lock Management

### Lock TTL (Time-To-Live)

**Purpose**: Prevent indefinite blocking

**File**: Lock cleanup in TiKV

```go
type Lock struct {
    StartTS  uint64
    Primary  []byte
    TTL      uint64  // Milliseconds
}
```

**Default TTL**: 3 seconds

**What happens on timeout**:
```
1. TiKV detects lock expired
2. TiKV resolves lock:
   - Check if primary committed
   - If yes: Commit secondary
   - If no: Rollback secondary
```

### Lock Resolution

**File**: [`pkg/sessiontxn/isolation/base.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessiontxn/isolation/base.go)

**When reader encounters lock**:
```go
func (r *Reader) resolveLock(lock *Lock) error {
    // 1. Check primary key status
    primary := lock.Primary
    primaryStatus := checkPrimaryStatus(primary)

    switch primaryStatus {
    case Committed:
        // Commit this secondary key
        return commitSecondary(lock, primaryStatus.CommitTS)

    case Rolled

Back:
        // Rollback this secondary key
        return rollbackSecondary(lock)

    case Locked:
        // Primary still locked, wait or return conflict
        if lock.TTL > 0 {
            return ErrLockWait
        } else {
            // TTL expired, force rollback
            return rollbackTransaction(lock)
        }
    }
}
```

---

## Transaction Isolation Levels

**File**: [`pkg/sessionctx/variable/sysvar.go`](https://github.com/pingcap/tidb/blob/bd6aa865/pkg/sessionctx/variable/sysvar.go)

**Supported**:
```sql
SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED;
SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ;  -- Default
```

**Repeatable Read**: Snapshot isolation
- All reads see snapshot at start_ts
- Writes checked at commit

**Read Committed**: Statement-level snapshots
- Each statement gets new snapshot
- Less strict, higher concurrency

---

## Real-World Example

**Bank transfer**:
```sql
BEGIN;
UPDATE accounts SET balance = balance - 100 WHERE id = 1;  -- Alice
UPDATE accounts SET balance = balance + 100 WHERE id = 2;  -- Bob
COMMIT;
```

**Under the hood**:
```
1. Get start_ts from PD: 1000

2. Execute UPDATE (Alice):
   - Read balance at ts=1000: 500
   - Buffer write: id=1, new_balance=400

3. Execute UPDATE (Bob):
   - Read balance at ts=1000: 200
   - Buffer write: id=2, new_balance=300

4. COMMIT:
   - Prewrite phase:
     * Primary (id=1): Lock + write intent
     * Secondary (id=2): Lock + write intent
     * Check conflicts: None
   - Get commit_ts from PD: 1005
   - Commit phase:
     * Commit primary (id=1): Write @1005
     * Async commit secondary (id=2): Write @1005

5. Transaction durable!
```

**If concurrent transfer**:
```
T1: Alice → Bob ($100)
T2: Alice → Charlie ($50)

Both start_ts = 1000

T1 commits first (commit_ts = 1005)
T2 prewrite fails: Alice's balance changed after 1000

T2 retries with new start_ts = 1010
Sees Alice's new balance (400)
Succeeds
```

---

## Performance Characteristics

**Latency breakdown**:
```
Optimistic transaction:
- BEGIN: 1 PD RPC (get start_ts) = 1ms
- Reads: TiKV RPC per read = 1-2ms each
- COMMIT:
  - Prewrite: 1 TiKV RPC per key = 2-5ms
  - Commit: 1 PD RPC (get commit_ts) + 1 TiKV RPC (primary) = 2ms
Total: ~10-20ms

Pessimistic transaction:
- BEGIN: 1 PD RPC = 1ms
- First write: Lock acquisition = 2-5ms
- Subsequent writes: Same
- COMMIT: 1 PD RPC + 1 TiKV RPC = 2ms
Total: Higher, but predictable
```

**Throughput**:
- Optimistic: 100K+ TPS (low contention)
- Pessimistic: 50K+ TPS (controlled contention)

---

## Key Takeaways

1. **TSO provides global ordering** via PD
2. **2PC ensures atomicity** across distributed nodes
3. **MVCC enables lock-free reads** via versioning
4. **Optimistic mode** good for low contention
5. **Pessimistic mode** good for high contention
6. **Lock TTL prevents deadlocks** in distributed system
7. **GC cleans old versions** to reclaim storage

---

## Best Practices

1. **Choose transaction mode based on workload**:
   - OLTP with low contention → Optimistic
   - OLTP with high contention → Pessimistic

2. **Keep transactions short**: Reduces conflict probability

3. **Batch operations**: Use batch insert/update for better throughput

4. **Handle retries gracefully**: Implement exponential backoff

5. **Monitor transaction metrics**:
   ```sql
   SELECT * FROM information_schema.tidb_trx;
   ```

---

## Next Steps

**Continue Reading**:
- **[Part 5: Execution Engine Internals](./05-execution-engine.md)**

**Experiment**:
```sql
-- Force pessimistic mode
BEGIN PESSIMISTIC;
SELECT * FROM accounts WHERE id = 1 FOR UPDATE;
UPDATE accounts SET balance = balance - 100 WHERE id = 1;
COMMIT;

-- Check transaction status
SELECT * FROM information_schema.tidb_trx;
```

---

**Part 4 of 7 - Next**: [Execution Engine →](./05-execution-engine.md)
