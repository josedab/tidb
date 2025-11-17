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

package metrics

import (
	metricscommon "github.com/pingcap/tidb/pkg/metrics/common"
	"github.com/prometheus/client_golang/prometheus"
)

// Chunk pool metrics for monitoring memory optimization
var (
	// ChunkPoolHits counts successful chunk retrievals from pool
	ChunkPoolHits *prometheus.CounterVec

	// ChunkPoolMisses counts when new chunks must be allocated
	ChunkPoolMisses prometheus.Counter

	// ChunkPoolSize tracks current pool sizes
	ChunkPoolSize *prometheus.GaugeVec

	// ChunkPoolDrops counts chunks not pooled (too large, memory pressure, etc.)
	ChunkPoolDrops *prometheus.CounterVec
)

// InitChunkMetrics initializes chunk pool metrics
func InitChunkMetrics() {
	ChunkPoolHits = metricscommon.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tidb",
			Subsystem: "chunk",
			Name:      "pool_hits_total",
			Help:      "Chunk pool hit count by pool type (local, global)",
		}, []string{"pool"},
	)

	ChunkPoolMisses = metricscommon.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tidb",
			Subsystem: "chunk",
			Name:      "pool_misses_total",
			Help:      "Chunk pool miss count (new allocation required)",
		},
	)

	ChunkPoolSize = metricscommon.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "chunk",
			Name:      "pool_size",
			Help:      "Current chunk pool size by type (active, pooled)",
		}, []string{"type"},
	)

	ChunkPoolDrops = metricscommon.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tidb",
			Subsystem: "chunk",
			Name:      "pool_drops_total",
			Help:      "Chunks dropped (not pooled) by reason (size_limit, memory_pressure, pool_limit)",
		}, []string{"reason"},
	)
}
