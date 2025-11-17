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

package error

import (
	"testing"

	deadlockpb "github.com/pingcap/kvproto/pkg/deadlock"
	"github.com/pingcap/kvproto/pkg/kvrpcpb"
	"github.com/stretchr/testify/require"
	tikverr "github.com/tikv/client-go/v2/error"
)

func TestConvertDeadlockError(t *testing.T) {
	// Create a mock deadlock error from TiKV
	tikvErr := &tikverr.ErrDeadlock{
		Deadlock: &kvrpcpb.Deadlock{
			LockTs:          100,
			LockKey:         []byte("key1"),
			DeadlockKeyHash: 12345,
			WaitChain: []*deadlockpb.WaitForEntry{
				{
					Txn:              100,
					WaitForTxn:       200,
					Key:              []byte{0x74, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
					ResourceGroupTag: []byte{},
				},
				{
					Txn:              200,
					WaitForTxn:       100,
					Key:              []byte{0x74, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
					ResourceGroupTag: []byte{},
				},
			},
		},
		IsRetryable: false,
	}

	// Convert to TiDB error
	err := ConvertDeadlockError(tikvErr)

	// Verify error is not nil
	require.NotNil(t, err)

	// Verify error message contains expected information
	errMsg := err.Error()
	require.Contains(t, errMsg, "DEADLOCK DETECTED")
	require.Contains(t, errMsg, "Transaction 1 (start_ts=100)")
	require.Contains(t, errMsg, "Transaction 2 (start_ts=200)")
	require.Contains(t, errMsg, "was chosen as deadlock victim")
	require.Contains(t, errMsg, "Please retry your transaction")
}

func TestConvertDeadlockErrorWithRetryable(t *testing.T) {
	// Create a retryable deadlock error
	tikvErr := &tikverr.ErrDeadlock{
		Deadlock: &kvrpcpb.Deadlock{
			LockTs:          100,
			LockKey:         []byte("key1"),
			DeadlockKeyHash: 12345,
			WaitChain: []*deadlockpb.WaitForEntry{
				{
					Txn:              150,
					WaitForTxn:       250,
					Key:              []byte("key1"),
					ResourceGroupTag: []byte{},
				},
				{
					Txn:              250,
					WaitForTxn:       150,
					Key:              []byte("key2"),
					ResourceGroupTag: []byte{},
				},
			},
		},
		IsRetryable: true,
	}

	// Convert to TiDB error
	err := ConvertDeadlockError(tikvErr)

	// Verify error contains retryable information
	errMsg := err.Error()
	require.Contains(t, errMsg, "Retryable: true")
	// The younger transaction (250) should be selected as victim
	require.Contains(t, errMsg, "start_ts=250) was chosen as deadlock victim")
}

func TestConvertDeadlockErrorThreeWay(t *testing.T) {
	// Create a three-way deadlock
	tikvErr := &tikverr.ErrDeadlock{
		Deadlock: &kvrpcpb.Deadlock{
			LockTs:          100,
			LockKey:         []byte("key1"),
			DeadlockKeyHash: 12345,
			WaitChain: []*deadlockpb.WaitForEntry{
				{
					Txn:              100,
					WaitForTxn:       200,
					Key:              []byte("key1"),
					ResourceGroupTag: []byte{},
				},
				{
					Txn:              200,
					WaitForTxn:       300,
					Key:              []byte("key2"),
					ResourceGroupTag: []byte{},
				},
				{
					Txn:              300,
					WaitForTxn:       100,
					Key:              []byte("key3"),
					ResourceGroupTag: []byte{},
				},
			},
		},
		IsRetryable: false,
	}

	// Convert to TiDB error
	err := ConvertDeadlockError(tikvErr)

	// Verify all three transactions are mentioned
	errMsg := err.Error()
	require.Contains(t, errMsg, "Transaction 1 (start_ts=100)")
	require.Contains(t, errMsg, "Transaction 2 (start_ts=200)")
	require.Contains(t, errMsg, "Transaction 3 (start_ts=300)")
	// The youngest transaction (300) should be selected as victim
	require.Contains(t, errMsg, "start_ts=300) was chosen as deadlock victim")
}

func TestToTiDBErrWithDeadlock(t *testing.T) {
	// Test that ToTiDBErr properly converts deadlock errors
	tikvErr := &tikverr.ErrDeadlock{
		Deadlock: &kvrpcpb.Deadlock{
			LockTs:          100,
			LockKey:         []byte("key1"),
			DeadlockKeyHash: 12345,
			WaitChain: []*deadlockpb.WaitForEntry{
				{
					Txn:              100,
					WaitForTxn:       200,
					Key:              []byte("key1"),
					ResourceGroupTag: []byte{},
				},
				{
					Txn:              200,
					WaitForTxn:       100,
					Key:              []byte("key2"),
					ResourceGroupTag: []byte{},
				},
			},
		},
		IsRetryable: false,
	}

	// Convert using ToTiDBErr
	err := ToTiDBErr(tikvErr)

	// Verify it was converted properly
	require.NotNil(t, err)
	errMsg := err.Error()
	require.Contains(t, errMsg, "DEADLOCK DETECTED")
}
