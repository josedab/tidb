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

package plancache

import (
	"context"
	"fmt"
	"time"

	"github.com/pingcap/errors"
	"github.com/pingcap/tidb/pkg/config"
	"github.com/pingcap/tidb/pkg/kv"
	"github.com/pingcap/tidb/pkg/metrics"
	"github.com/pingcap/tidb/pkg/sessionctx"
	"github.com/pingcap/tidb/pkg/util"
	"github.com/pingcap/tidb/pkg/util/chunk"
	"github.com/pingcap/tidb/pkg/util/logutil"
	"go.uber.org/zap"
)

// WarmupQuery represents a query to be warmed up in the plan cache
type WarmupQuery struct {
	SQL            string
	SchemaName     string
	ExecutionCount int64
	AvgLatency     time.Duration
}

// DomainInterface defines the interface needed from domain for warmup
type DomainInterface interface {
	// SysSessionPool returns the system session pool
	SysSessionPool() util.DestroyableSessionPool
}

// WarmupPlanCache compiles and caches plans for top queries during server startup
func WarmupPlanCache(ctx context.Context, dom DomainInterface, cfg config.PlanCacheWarmupConfig) error {
	logutil.BgLogger().Info("Starting plan cache warmup", zap.Any("config", cfg))

	startTime := time.Now()
	timeout := time.Duration(cfg.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Extract top queries
	queries, err := ExtractTopQueries(ctx, dom, cfg)
	if err != nil {
		return errors.Trace(err)
	}

	logutil.BgLogger().Info("Extracted queries for warmup", zap.Int("count", len(queries)))

	// Get session from pool for plan compilation
	resource, err := dom.SysSessionPool().Get()
	if err != nil {
		return err
	}
	defer dom.SysSessionPool().Put(resource)

	sess, ok := resource.(sessionctx.Context)
	if !ok {
		return errors.New("failed to convert resource to session context")
	}

	successCount := 0
	errorCount := 0

	for i, q := range queries {
		// Check timeout
		select {
		case <-ctx.Done():
			logutil.BgLogger().Warn("Plan cache warmup timeout",
				zap.Duration("elapsed", time.Since(startTime)),
				zap.Int("warmed", successCount))
			return nil
		default:
		}

		// Set schema
		if q.SchemaName != "" {
			_, err := executeSQL(ctx, sess, "USE "+q.SchemaName)
			if err != nil {
				logutil.BgLogger().Warn("Failed to switch schema",
					zap.String("schema", q.SchemaName),
					zap.Error(err))
				continue
			}
		}

		// Compile plan (this populates the cache)
		_, err := executeSQL(ctx, sess, "EXPLAIN "+q.SQL)
		if err != nil {
			errorCount++
			logutil.BgLogger().Debug("Failed to warm up query",
				zap.Int("index", i),
				zap.String("sql", truncateSQL(q.SQL, 100)),
				zap.Error(err))
			continue
		}

		successCount++

		// Throttle to avoid CPU spike
		if i%10 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	elapsed := time.Since(startTime)
	logutil.BgLogger().Info("Plan cache warmup completed",
		zap.Int("total", len(queries)),
		zap.Int("success", successCount),
		zap.Int("errors", errorCount),
		zap.Duration("elapsed", elapsed))

	// Emit metrics
	metrics.PlanCacheWarmupDuration.Observe(elapsed.Seconds())
	metrics.PlanCacheWarmupCount.WithLabelValues("success").Add(float64(successCount))
	metrics.PlanCacheWarmupCount.WithLabelValues("error").Add(float64(errorCount))

	return nil
}

// ExtractTopQueries retrieves most frequently executed queries
func ExtractTopQueries(ctx context.Context, dom DomainInterface, cfg config.PlanCacheWarmupConfig) ([]WarmupQuery, error) {
	switch cfg.Source {
	case "statement-summary":
		return extractFromStatementSummary(ctx, dom, cfg)
	case "slow-log":
		return extractFromSlowLog(ctx, cfg)
	default:
		return nil, errors.Errorf("unknown warmup source: %s", cfg.Source)
	}
}

// extractFromStatementSummary retrieves top queries from statement summary
func extractFromStatementSummary(ctx context.Context, dom DomainInterface, cfg config.PlanCacheWarmupConfig) ([]WarmupQuery, error) {
	// Get session from pool
	resource, err := dom.SysSessionPool().Get()
	if err != nil {
		return nil, err
	}
	defer dom.SysSessionPool().Put(resource)

	sess, ok := resource.(sessionctx.Context)
	if !ok {
		return nil, errors.New("failed to convert resource to session context")
	}

	// Query statement summary for top queries
	sql := fmt.Sprintf(`
		SELECT DIGEST_TEXT, SCHEMA_NAME, EXEC_COUNT, AVG_LATENCY
		FROM information_schema.statements_summary
		WHERE EXEC_COUNT >= %d
		ORDER BY EXEC_COUNT DESC
		LIMIT %d
	`, cfg.MinExecutions, cfg.TopN)

	rows, err := executeSQL(ctx, sess, sql)
	if err != nil {
		return nil, err
	}

	var queries []WarmupQuery
	for _, row := range rows {
		if row.Len() < 4 {
			continue
		}

		queries = append(queries, WarmupQuery{
			SQL:            row.GetString(0),
			SchemaName:     row.GetString(1),
			ExecutionCount: row.GetInt64(2),
			AvgLatency:     time.Duration(row.GetInt64(3)),
		})
	}

	return queries, nil
}

// extractFromSlowLog retrieves top queries from slow query log
func extractFromSlowLog(ctx context.Context, cfg config.PlanCacheWarmupConfig) ([]WarmupQuery, error) {
	// TODO: Implement slow log parsing
	// For now, return empty list as statement-summary is the preferred source
	logutil.BgLogger().Info("Slow log warmup source not yet implemented, using empty list")
	return []WarmupQuery{}, nil
}

// executeSQL executes a SQL statement and returns the result rows
func executeSQL(ctx context.Context, sess sessionctx.Context, sql string) ([]chunk.Row, error) {
	if sess == nil {
		return nil, errors.New("session is nil")
	}

	// Get restricted SQL executor
	exec := sess.GetRestrictedSQLExecutor()
	if exec == nil {
		return nil, errors.New("restricted SQL executor is nil")
	}

	// Execute SQL with internal source type
	ctx = kv.WithInternalSourceType(ctx, kv.InternalTxnOthers)
	rows, _, err := exec.ExecRestrictedSQL(ctx, nil, sql)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return rows, nil
}

// truncateSQL truncates SQL string to specified length
func truncateSQL(sql string, maxLen int) string {
	if len(sql) <= maxLen {
		return sql
	}
	return sql[:maxLen] + "..."
}
