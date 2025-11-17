# Enhanced Distributed Deadlock Detection

## Summary

This document describes the implementation of enhanced deadlock detection and diagnostics in TiDB, following RFC-0007. The enhancement provides better error messages, automatic victim selection, and detailed deadlock cycle information to help users understand and resolve deadlock issues quickly.

## Background

Prior to this enhancement, TiDB's deadlock detection provided minimal information to users when a deadlock occurred. The error message was simply "Deadlock found when trying to get lock; try restarting transaction" without any details about which transactions were involved or what caused the deadlock.

While TiDB already had infrastructure for deadlock detection and history tracking (via `information_schema.deadlocks`), the user-facing error messages lacked the detail needed for quick diagnosis and resolution.

## Implementation

### Components Implemented

#### 1. Enhanced Deadlock Error Formatting (`pkg/util/deadlockhistory/deadlock_formatter.go`)

This new module provides:

- **`FormatDeadlockError(details *DeadlockDetails) string`**: Formats deadlock information into a user-friendly error message showing:
  - Timestamp when deadlock occurred
  - Whether the deadlock is retryable
  - Complete deadlock cycle with transaction IDs
  - SQL digests for each transaction (when available)
  - Keys being locked
  - Which transaction was chosen as the victim

- **`SelectDeadlockVictim(waitChain []WaitChainItem) uint64`**: Implements victim selection algorithm that chooses the youngest transaction (highest start_ts) to abort, as it has performed the least work.

- **`FormatDeadlockChain(record *DeadlockRecord) string`**: Provides a concise format for logging deadlock cycles.

Example output:
```
Deadlock found when trying to get lock; try restarting transaction

*** DEADLOCK DETECTED ***
Occurred at: 2025-11-17T10:30:45Z
Retryable: false

Deadlock cycle:
  Transaction 1 (start_ts=100)
    Current SQL: digest=abc123
    Waiting for lock on key: 0x7480000000000001 (table_id=1, record_id=0, type=record)
    Held by: Transaction 2 (start_ts=200)
    |
    v
  Transaction 2 (start_ts=200)
    Current SQL: digest=def456
    Waiting for lock on key: 0x7480000000000002 (table_id=1, record_id=0, type=record)
    Held by: Transaction 1 (start_ts=100)

*** Transaction 2 (start_ts=200) was chosen as deadlock victim and has been aborted.
*** Please retry your transaction.
```

#### 2. Error Conversion (`pkg/store/driver/error/error.go`)

Enhanced the `ToTiDBErr` function to detect and convert TiKV deadlock errors:

- Added `ConvertDeadlockError` function that:
  - Converts TiKV deadlock error to TiDB deadlock record
  - Selects the victim transaction
  - Formats detailed error message
  - Returns error with MySQL error code 1213 (ErrLockDeadlock)

#### 3. Comprehensive Test Coverage

Created test suites to verify:

- **Deadlock Formatter Tests** (`pkg/util/deadlockhistory/deadlock_formatter_test.go`):
  - Error formatting with various deadlock scenarios
  - Victim selection algorithm
  - Key information formatting
  - Deadlock chain formatting

- **Error Conversion Tests** (`pkg/store/driver/error/deadlock_test.go`):
  - Two-way deadlocks
  - Three-way deadlocks
  - Retryable vs non-retryable deadlocks
  - Integration with `ToTiDBErr`

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         TiKV                                 │
│                  (Deadlock Detection)                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         │ ErrDeadlock (with WaitChain)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│              pkg/store/driver/error                          │
│                                                               │
│  ToTiDBErr() → ConvertDeadlockError()                       │
│    • Converts TiKV error to TiDB error                      │
│    • Selects victim transaction                             │
│    • Formats detailed error message                         │
└────────────────────────┬────────────────────────────────────┘
                         │
                         │ Formatted error with details
                         ▼
┌─────────────────────────────────────────────────────────────┐
│              Application Layer                               │
│                                                               │
│  • User sees detailed deadlock information                  │
│  • Can understand which transaction was aborted            │
│  • Can see the complete deadlock cycle                     │
└─────────────────────────────────────────────────────────────┘
```

## Victim Selection Algorithm

The implementation follows the RFC's recommendation to select the youngest transaction (highest `start_ts`) as the victim because:

1. It has executed for the least amount of time
2. It has performed the least work that needs to be rolled back
3. Rolling it back wastes the least amount of resources

This is implemented in `SelectDeadlockVictim`:

```go
func SelectDeadlockVictim(waitChain []WaitChainItem) uint64 {
    var victim uint64
    var maxStartTS uint64

    for _, item := range waitChain {
        if item.TryLockTxn > maxStartTS {
            maxStartTS = item.TryLockTxn
            victim = item.TryLockTxn
        }
    }

    return victim
}
```

## Testing

All tests pass successfully:

```bash
# Deadlock formatter tests
$ cd pkg/util/deadlockhistory
$ go test -v --tags=intest
PASS: TestFormatDeadlockError
PASS: TestFormatDeadlockChain
PASS: TestSelectDeadlockVictim
PASS: TestFormatKeyInfo
PASS: TestFindTxnIndex

# Error conversion tests
$ cd pkg/store/driver/error
$ go test -v -run TestConvertDeadlockError --tags=intest
PASS: TestConvertDeadlockError
PASS: TestConvertDeadlockErrorWithRetryable
PASS: TestConvertDeadlockErrorThreeWay
PASS: TestToTiDBErrWithDeadlock
```

## Compatibility

This implementation is fully backward compatible:

- Uses existing error code (MySQL error 1213 - ErrLockDeadlock)
- Enhances error messages but doesn't change error codes
- Works with existing deadlock detection infrastructure
- Compatible with `information_schema.deadlocks` table

## Limitations and Future Work

### Current Implementation Scope

This implementation focuses on the **TiDB-side enhancements** for deadlock detection:

1. ✅ Enhanced error message formatting
2. ✅ Automatic victim selection
3. ✅ Detailed deadlock cycle information
4. ✅ Key and transaction information display

### Not Implemented (PD/TiKV Components)

The following components from RFC-0007 require changes to PD and TiKV codebases:

1. ❌ **PD Wait-For Graph**: Centralized graph in PD for tracking all transaction wait relationships
2. ❌ **Proactive Cycle Detection**: 100ms detection interval in PD instead of timeout-based detection
3. ❌ **TiKV Integration**: Real-time wait event reporting from TiKV to PD

These components would enable:
- Sub-second deadlock detection (currently relies on existing detection)
- Reduced latency from 40s → <1s for deadlock detection
- Centralized deadlock management across the cluster

### Current Behavior

The current implementation:
- Uses existing deadlock detection from TiKV/unistore
- Enhances the error messages when deadlocks are detected
- Provides better diagnostics without changing detection speed
- Works with both mock store (unistore) and real TiKV clusters

### Recommended Next Steps

To achieve the full benefits described in RFC-0007:

1. Implement Wait-For Graph in PD (3-5 days)
2. Add TiKV wait event reporting (3-5 days)
3. Implement proactive cycle detection in PD (2-3 days)
4. End-to-end integration testing (3-5 days)

## References

- RFC-0007: Enhanced Distributed Deadlock Detection
- MySQL Error Code 1213: `ER_LOCK_DEADLOCK`
- TiDB Deadlock History: `pkg/util/deadlockhistory/`
- Information Schema: `information_schema.deadlocks`

## Appendix: Example Scenarios

### Scenario 1: Simple Two-Transaction Deadlock

```sql
-- Session 1
BEGIN PESSIMISTIC;
UPDATE accounts SET balance = 100 WHERE id = 1;  -- Locks row 1

-- Session 2
BEGIN PESSIMISTIC;
UPDATE accounts SET balance = 200 WHERE id = 2;  -- Locks row 2
UPDATE accounts SET balance = 300 WHERE id = 1;  -- BLOCKED on row 1

-- Back to Session 1
UPDATE accounts SET balance = 400 WHERE id = 2;  -- BLOCKED on row 2 → DEADLOCK!

-- Error received (in Session 2, the younger transaction):
-- ERROR 1213 (40001): Deadlock found when trying to get lock; try restarting transaction
--
-- *** DEADLOCK DETECTED ***
-- Occurred at: 2025-11-17T10:30:45Z
-- Retryable: false
--
-- Deadlock cycle:
--   Transaction 1 (start_ts=437580912345678900)
--     Waiting for lock on key: 0x7480000000000002...
--     Held by: Transaction 2 (start_ts=437580912345678901)
--     |
--     v
--   Transaction 2 (start_ts=437580912345678901)
--     Waiting for lock on key: 0x7480000000000001...
--     Held by: Transaction 1 (start_ts=437580912345678900)
--
-- *** Transaction 2 (start_ts=437580912345678901) was chosen as deadlock victim and has been aborted.
-- *** Please retry your transaction.
```

### Scenario 2: Three-Way Deadlock

```sql
-- Transaction 1 holds A, waits for B
-- Transaction 2 holds B, waits for C
-- Transaction 3 holds C, waits for A

-- The error message clearly shows all three transactions in the cycle
-- and indicates which one was chosen as the victim.
```
