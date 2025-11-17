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

package handle

import (
	"context"

	"github.com/pingcap/tidb/pkg/infoschema"
	"github.com/pingcap/tidb/pkg/meta/model"
	"github.com/pingcap/tidb/pkg/sessionctx"
	"github.com/pingcap/tidb/pkg/statistics"
	"go.uber.org/zap"
)

// IncrementalAnalyzer performs incremental statistics analysis
type IncrementalAnalyzer struct {
	statsHandle StatsHandle
	logger      *zap.Logger
}

// StatsHandle interface for accessing statistics
type StatsHandle interface {
	GetPartitionStats(tableID int64, partID int64) *statistics.Table
	GetTableStats(tblID int64) *statistics.Table
}

// NewIncrementalAnalyzer creates an incremental analyzer
func NewIncrementalAnalyzer(handle StatsHandle, logger *zap.Logger) *IncrementalAnalyzer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &IncrementalAnalyzer{
		statsHandle: handle,
		logger:      logger,
	}
}

// AnalyzeTableIncremental performs incremental analysis
func (ia *IncrementalAnalyzer) AnalyzeTableIncremental(
	ctx context.Context,
	sctx sessionctx.Context,
	tbl *model.TableInfo,
	is infoschema.InfoSchema,
) error {
	// Check if table is partitioned
	if tbl.Partition != nil {
		return ia.analyzePartitionedTable(ctx, sctx, tbl, is)
	}

	// For regular tables, use range-based incremental analysis
	return ia.analyzeRegularTableIncremental(ctx, sctx, tbl, is)
}

// analyzePartitionedTable analyzes only modified partitions
func (ia *IncrementalAnalyzer) analyzePartitionedTable(
	ctx context.Context,
	sctx sessionctx.Context,
	tbl *model.TableInfo,
	is infoschema.InfoSchema,
) error {
	modifiedPartitions := ia.findModifiedPartitions(tbl)

	ia.logger.Info("analyzing partitioned table incrementally",
		zap.String("table", tbl.Name.O),
		zap.Int("modified_partitions", len(modifiedPartitions)))

	for _, part := range modifiedPartitions {
		// Analyze single partition
		err := ia.analyzePartition(ctx, sctx, tbl, part, is)
		if err != nil {
			ia.logger.Error("failed to analyze partition",
				zap.String("table", tbl.Name.O),
				zap.Int64("partition_id", part.ID),
				zap.Error(err))
			return err
		}

		ia.logger.Info("partition analyzed",
			zap.String("table", tbl.Name.O),
			zap.Int64("partition_id", part.ID))
	}

	return nil
}

// findModifiedPartitions identifies partitions with modifications
func (ia *IncrementalAnalyzer) findModifiedPartitions(tbl *model.TableInfo) []*model.PartitionDefinition {
	var modified []*model.PartitionDefinition

	if tbl.Partition == nil {
		return modified
	}

	for i := range tbl.Partition.Definitions {
		part := &tbl.Partition.Definitions[i]
		// Check modify count for partition
		stats := ia.statsHandle.GetPartitionStats(tbl.ID, part.ID)
		if stats != nil && stats.ModifyCount > 0 {
			modified = append(modified, part)
		}
	}

	return modified
}

// analyzePartition analyzes a single partition
// This is a simplified implementation - actual implementation would execute
// ANALYZE TABLE ... PARTITION (partition_name)
func (ia *IncrementalAnalyzer) analyzePartition(
	ctx context.Context,
	sctx sessionctx.Context,
	tbl *model.TableInfo,
	part *model.PartitionDefinition,
	is infoschema.InfoSchema,
) error {
	// In a real implementation, this would:
	// 1. Build SQL: ANALYZE TABLE tbl PARTITION (part_name)
	// 2. Execute the analysis
	// 3. Update statistics
	//
	// For this RFC implementation, we log the operation
	ia.logger.Info("would analyze partition",
		zap.String("table", tbl.Name.O),
		zap.String("partition", part.Name.O),
		zap.Int64("partition_id", part.ID))

	return nil
}

// analyzeRegularTableIncremental performs incremental analysis for non-partitioned tables
func (ia *IncrementalAnalyzer) analyzeRegularTableIncremental(
	ctx context.Context,
	sctx sessionctx.Context,
	tbl *model.TableInfo,
	is infoschema.InfoSchema,
) error {
	stats := ia.statsHandle.GetTableStats(tbl.ID)
	if stats == nil {
		// No existing stats, need full analysis
		ia.logger.Info("no existing stats, recommend full analysis",
			zap.String("table", tbl.Name.O))
		return nil
	}

	modifyRatio := float64(stats.ModifyCount) / float64(stats.Count)

	ia.logger.Info("analyzing table incrementally",
		zap.String("table", tbl.Name.O),
		zap.Float64("modify_ratio", modifyRatio),
		zap.Int64("modify_count", stats.ModifyCount),
		zap.Int64("total_count", stats.Count))

	switch {
	case modifyRatio < 0.1:
		// < 10% modified: Delta-based approach
		return ia.analyzeDelta(ctx, sctx, tbl, stats)

	case modifyRatio < 0.5:
		// 10-50% modified: Hybrid approach (sampling)
		return ia.analyzeHybrid(ctx, sctx, tbl, stats)

	default:
		// > 50% modified: Full analysis more efficient
		ia.logger.Info("high modify ratio, recommend full analysis",
			zap.String("table", tbl.Name.O),
			zap.Float64("modify_ratio", modifyRatio))
		return nil
	}
}

// analyzeDelta analyzes only new/modified rows and merges with existing stats
func (ia *IncrementalAnalyzer) analyzeDelta(
	ctx context.Context,
	sctx sessionctx.Context,
	tbl *model.TableInfo,
	existingStats *statistics.Table,
) error {
	ia.logger.Info("using delta analysis",
		zap.String("table", tbl.Name.O))

	// In a real implementation, this would:
	// 1. Identify range of modified rows
	// 2. Build delta statistics for those rows
	// 3. Merge with existing statistics
	//
	// This is a simplified placeholder
	return nil
}

// analyzeHybrid uses sampling + existing stats
func (ia *IncrementalAnalyzer) analyzeHybrid(
	ctx context.Context,
	sctx sessionctx.Context,
	tbl *model.TableInfo,
	existingStats *statistics.Table,
) error {
	ia.logger.Info("using hybrid analysis with sampling",
		zap.String("table", tbl.Name.O))

	// In a real implementation, this would:
	// 1. Sample 20-30% of table
	// 2. Combine with existing stats (weighted merge)
	// 3. Update statistics
	//
	// This is faster than full scan, more accurate than pure delta
	return nil
}

// ShouldUseIncrementalAnalysis determines if incremental analysis is beneficial
func (ia *IncrementalAnalyzer) ShouldUseIncrementalAnalysis(tbl *model.TableInfo) bool {
	// Partitioned tables always benefit from incremental analysis
	if tbl.Partition != nil {
		return true
	}

	// For regular tables, check if stats exist and modify ratio
	stats := ia.statsHandle.GetTableStats(tbl.ID)
	if stats == nil {
		return false
	}

	modifyRatio := float64(stats.ModifyCount) / float64(stats.Count)
	// Use incremental if less than 50% modified
	return modifyRatio < 0.5
}
