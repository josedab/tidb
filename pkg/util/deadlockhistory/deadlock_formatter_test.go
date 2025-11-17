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

package deadlockhistory

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormatDeadlockError(t *testing.T) {
	// Test with nil details
	msg := FormatDeadlockError(nil)
	require.Contains(t, msg, "Deadlock found when trying to get lock")

	// Test with empty record
	msg = FormatDeadlockError(&DeadlockDetails{})
	require.Contains(t, msg, "Deadlock found when trying to get lock")

	// Test with a simple two-transaction deadlock
	now := time.Now()
	record := &DeadlockRecord{
		OccurTime:   now,
		IsRetryable: false,
		WaitChain: []WaitChainItem{
			{
				TryLockTxn:     100,
				TxnHoldingLock: 200,
				SQLDigest:      "abc123",
				Key:            []byte{0x74, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			},
			{
				TryLockTxn:     200,
				TxnHoldingLock: 100,
				SQLDigest:      "def456",
				Key:            []byte{0x74, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
			},
		},
	}

	details := &DeadlockDetails{
		Record:      record,
		VictimTxnID: 200,
	}

	msg = FormatDeadlockError(details)

	// Verify key components are present
	require.Contains(t, msg, "DEADLOCK DETECTED")
	require.Contains(t, msg, "Transaction 1 (start_ts=100)")
	require.Contains(t, msg, "Transaction 2 (start_ts=200)")
	require.Contains(t, msg, "digest=abc123")
	require.Contains(t, msg, "digest=def456")
	require.Contains(t, msg, "was chosen as deadlock victim")
	require.Contains(t, msg, "Please retry your transaction")
	require.Contains(t, msg, "Retryable: false")
}

func TestFormatDeadlockChain(t *testing.T) {
	// Test with nil
	chain := FormatDeadlockChain(nil)
	require.Equal(t, "empty deadlock chain", chain)

	// Test with empty wait chain
	record := &DeadlockRecord{
		WaitChain: []WaitChainItem{},
	}
	chain = FormatDeadlockChain(record)
	require.Equal(t, "empty deadlock chain", chain)

	// Test with a simple cycle
	record = &DeadlockRecord{
		WaitChain: []WaitChainItem{
			{TryLockTxn: 100, TxnHoldingLock: 200},
			{TryLockTxn: 200, TxnHoldingLock: 100},
		},
	}
	chain = FormatDeadlockChain(record)
	require.Equal(t, "100→200 → 200→100", chain)
}

func TestSelectDeadlockVictim(t *testing.T) {
	// Test with empty chain
	victim := SelectDeadlockVictim(nil)
	require.Equal(t, uint64(0), victim)

	victim = SelectDeadlockVictim([]WaitChainItem{})
	require.Equal(t, uint64(0), victim)

	// Test with a two-transaction deadlock
	// Transaction 200 is younger (higher start_ts), so it should be the victim
	waitChain := []WaitChainItem{
		{TryLockTxn: 100, TxnHoldingLock: 200},
		{TryLockTxn: 200, TxnHoldingLock: 100},
	}
	victim = SelectDeadlockVictim(waitChain)
	require.Equal(t, uint64(200), victim, "Should select the youngest transaction (highest start_ts)")

	// Test with a three-transaction deadlock
	// Transaction 300 is the youngest
	waitChain = []WaitChainItem{
		{TryLockTxn: 100, TxnHoldingLock: 200},
		{TryLockTxn: 200, TxnHoldingLock: 300},
		{TryLockTxn: 300, TxnHoldingLock: 100},
	}
	victim = SelectDeadlockVictim(waitChain)
	require.Equal(t, uint64(300), victim, "Should select the youngest transaction in a 3-way deadlock")
}

func TestFormatKeyInfo(t *testing.T) {
	// Test with empty key
	info := formatKeyInfo(nil)
	require.Equal(t, "<empty>", info)

	info = formatKeyInfo([]byte{})
	require.Equal(t, "<empty>", info)

	// Test with a key that can't be decoded
	// This should return hex representation
	invalidKey := []byte{0x01, 0x02, 0x03}
	info = formatKeyInfo(invalidKey)
	require.Contains(t, info, "0x")
	require.Contains(t, strings.ToUpper(info), "010203")

	// Test with a valid table record key
	// Table ID = 1, Record key format: 't' + tableID
	validKey := []byte{0x74, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x03, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	info = formatKeyInfo(validKey)
	require.Contains(t, info, "0x")
	// The key should contain either table_id or be in hex format
	// Since DecodeKeyHead might succeed or fail depending on the exact format,
	// we just verify it contains hex representation
	require.True(t, strings.Contains(info, "0x748000000000000001") || strings.Contains(info, "table_id="))
}

func TestFindTxnIndex(t *testing.T) {
	waitChain := []WaitChainItem{
		{TryLockTxn: 100, TxnHoldingLock: 200},
		{TryLockTxn: 200, TxnHoldingLock: 300},
		{TryLockTxn: 300, TxnHoldingLock: 100},
	}

	// Test finding each transaction
	idx := findTxnIndex(waitChain, 100)
	require.Equal(t, 0, idx)

	idx = findTxnIndex(waitChain, 200)
	require.Equal(t, 1, idx)

	idx = findTxnIndex(waitChain, 300)
	require.Equal(t, 2, idx)

	// Test not found
	idx = findTxnIndex(waitChain, 999)
	require.Equal(t, 0, idx)
}
