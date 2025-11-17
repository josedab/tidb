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
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pingcap/tidb/pkg/tablecodec"
)

// DeadlockDetails contains detailed information about a deadlock for user-facing error messages.
type DeadlockDetails struct {
	// The deadlock record containing the wait chain
	Record *DeadlockRecord
	// The transaction ID that was chosen as the victim
	VictimTxnID uint64
}

// FormatDeadlockError formats a deadlock error with detailed information about the deadlock cycle.
// This provides users with clear information about which transactions were involved and what they were waiting for.
func FormatDeadlockError(details *DeadlockDetails) string {
	if details == nil || details.Record == nil {
		return "Deadlock found when trying to get lock; try restarting transaction"
	}

	var buf strings.Builder
	buf.WriteString("Deadlock found when trying to get lock; try restarting transaction\n")
	buf.WriteString("\n")
	buf.WriteString("*** DEADLOCK DETECTED ***\n")
	buf.WriteString(fmt.Sprintf("Occurred at: %s\n", details.Record.OccurTime.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("Retryable: %v\n", details.Record.IsRetryable))
	buf.WriteString("\n")

	// Format the deadlock cycle
	buf.WriteString("Deadlock cycle:\n")
	for i, item := range details.Record.WaitChain {
		buf.WriteString(fmt.Sprintf("  Transaction %d (start_ts=%d)\n", i+1, item.TryLockTxn))

		// Format the SQL digest if available
		if item.SQLDigest != "" {
			buf.WriteString(fmt.Sprintf("    Current SQL: digest=%s\n", item.SQLDigest))
		}

		// Format the key being locked
		if len(item.Key) > 0 {
			keyInfo := formatKeyInfo(item.Key)
			buf.WriteString(fmt.Sprintf("    Waiting for lock on key: %s\n", keyInfo))
		}

		// Show which transaction holds the lock
		buf.WriteString(fmt.Sprintf("    Held by: Transaction %d (start_ts=%d)\n",
			(i+1)%len(details.Record.WaitChain)+1, item.TxnHoldingLock))

		if i < len(details.Record.WaitChain)-1 {
			buf.WriteString("    |\n")
			buf.WriteString("    v\n")
		}
	}

	buf.WriteString("\n")
	if details.VictimTxnID > 0 {
		buf.WriteString(fmt.Sprintf("*** Transaction %d (start_ts=%d) was chosen as deadlock victim and has been aborted.\n",
			findTxnIndex(details.Record.WaitChain, details.VictimTxnID)+1, details.VictimTxnID))
	}
	buf.WriteString("*** Please retry your transaction.\n")

	return buf.String()
}

// formatKeyInfo formats a key into a human-readable string with table and index information if possible.
func formatKeyInfo(key []byte) string {
	if len(key) == 0 {
		return "<empty>"
	}

	// Try to decode the key to get table/index information
	tableID, indexID, isRecord, err := tablecodec.DecodeKeyHead(key)
	if err != nil {
		// If we can't decode it, just return the hex representation
		return fmt.Sprintf("0x%s", strings.ToUpper(hex.EncodeToString(key)))
	}

	var keyType string
	if isRecord {
		keyType = "record"
	} else {
		keyType = "index"
	}

	return fmt.Sprintf("0x%s (table_id=%d, %s_id=%d, type=%s)",
		strings.ToUpper(hex.EncodeToString(key[:min(16, len(key))])),
		tableID, keyType, indexID, keyType)
}

// findTxnIndex finds the index of a transaction in the wait chain (1-based)
func findTxnIndex(waitChain []WaitChainItem, txnID uint64) int {
	for i, item := range waitChain {
		if item.TryLockTxn == txnID {
			return i
		}
	}
	return 0
}

// FormatDeadlockChain formats the deadlock chain for logging purposes.
// This is a more concise format suitable for logs.
func FormatDeadlockChain(record *DeadlockRecord) string {
	if record == nil || len(record.WaitChain) == 0 {
		return "empty deadlock chain"
	}

	var parts []string
	for _, item := range record.WaitChain {
		parts = append(parts, fmt.Sprintf("%d→%d", item.TryLockTxn, item.TxnHoldingLock))
	}
	return strings.Join(parts, " → ")
}

// SelectDeadlockVictim selects which transaction in a deadlock cycle should be aborted.
// According to the RFC, we should abort the youngest transaction (highest start_ts) as it has done the least work.
func SelectDeadlockVictim(waitChain []WaitChainItem) uint64 {
	if len(waitChain) == 0 {
		return 0
	}

	// Find the transaction with the highest start_ts (youngest transaction)
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
