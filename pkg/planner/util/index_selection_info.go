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

package util

import (
	"time"
)

// IndexSelectionInfo captures the index selection decision made during query optimization.
// It includes all candidate indexes considered, their costs, and the reason for selection/rejection.
type IndexSelectionInfo struct {
	// Candidates contains all possible access paths (indexes and table scan) evaluated
	Candidates []IndexCandidate
	// Chosen points to the selected candidate from the Candidates slice
	Chosen *IndexCandidate
	// Reason explains why the chosen candidate was selected (e.g., "lowest_cost")
	Reason string
	// StatsInfo captures statistics information used in the decision
	StatsInfo *StatisticsInfo
}

// IndexCandidate represents one possible index or access path choice.
type IndexCandidate struct {
	// IndexName is the name of the index, or "table_scan" for full table scan
	IndexName string
	// Cost is the estimated cost of using this index
	Cost float64
	// EstimatedRows is the number of rows expected to be scanned
	EstimatedRows int64
	// Selectivity is the proportion of rows that match (0.0 to 1.0)
	Selectivity float64
	// Chosen indicates if this candidate was selected
	Chosen bool
	// RejectedReason explains why this candidate was not chosen
	// Examples: "higher_cost", "no_stats", "column_not_in_index"
	RejectedReason string
}

// StatisticsInfo captures statistics information used in the index selection decision.
type StatisticsInfo struct {
	// Version is the statistics version number
	Version int64
	// HistogramBuckets is the number of buckets in the histogram
	HistogramBuckets int
	// LastUpdated is when statistics were last updated
	LastUpdated time.Time
	// Healthy indicates the health of statistics (0-100)
	Healthy int
}
