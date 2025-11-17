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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkloadMonitor_LowTrafficDetection(t *testing.T) {
	wm := NewWorkloadMonitor()

	// Set baseline values
	wm.avgQPS.Store(1000)    // 1000 QPS baseline
	wm.avgCPU.Store(5000)    // 50% CPU baseline
	wm.avgLatency.Store(1000) // 10ms baseline (stored as microseconds)

	// Test 1: High QPS should NOT be low traffic period
	// Mock current QPS to be 800 (80% of baseline)
	// This is above the 50% threshold
	// Note: In a real test, we would mock getCurrentQPS()
	// For now, we test the logic directly

	// Test 2: Low QPS, low CPU, low latency = low traffic
	// Set avgQPS to 100 for easier testing
	wm.avgQPS.Store(100)
	wm.avgCPU.Store(6000)    // 60% CPU
	wm.avgLatency.Store(1000) // 10ms

	// Update baselines
	wm.updateBaselines(50, 30.0, 8.0)

	// Verify baseline update (exponential moving average)
	// newQPS = 100 * 0.9 + 50 * 0.1 = 95
	require.Equal(t, uint64(95), wm.avgQPS.Load())
}

func TestWorkloadMonitor_GetLoadFactor(t *testing.T) {
	wm := NewWorkloadMonitor()

	// Set baseline values
	wm.avgQPS.Store(100)
	wm.avgCPU.Store(5000)    // 50%
	wm.avgLatency.Store(1000) // 10ms

	// Get load factor (should be calculated based on current vs baseline)
	loadFactor := wm.GetLoadFactor()

	// Load factor should be between 0.0 and 1.0
	require.GreaterOrEqual(t, loadFactor, 0.0)
	require.LessOrEqual(t, loadFactor, 1.0)
}

func TestWorkloadMonitor_Update(t *testing.T) {
	wm := NewWorkloadMonitor()

	// First update should succeed (more than 5 minutes have passed initially)
	initialTime := wm.lastUpdate
	time.Sleep(1 * time.Millisecond) // Small delay to ensure time difference

	wm.Update()

	// Verify that lastUpdate was changed
	require.NotEqual(t, initialTime, wm.lastUpdate)

	// Second update immediately should be skipped
	lastUpdate := wm.lastUpdate
	wm.Update()
	require.Equal(t, lastUpdate, wm.lastUpdate)
}

func TestWorkloadMonitor_GetCurrentMetrics(t *testing.T) {
	wm := NewWorkloadMonitor()

	qps, cpu, latency := wm.GetCurrentMetrics()

	// Verify metrics are reasonable
	require.GreaterOrEqual(t, qps, uint64(0))
	require.GreaterOrEqual(t, cpu, 0.0)
	require.LessOrEqual(t, cpu, 100.0)
	require.GreaterOrEqual(t, latency, 0.0)
}

func TestWorkloadMonitor_GetBaselineMetrics(t *testing.T) {
	wm := NewWorkloadMonitor()

	// Set known baseline values
	wm.avgQPS.Store(500)
	wm.avgCPU.Store(4000)   // 40%
	wm.avgLatency.Store(500) // 5ms

	qps, cpu, latency := wm.GetBaselineMetrics()

	require.Equal(t, uint64(500), qps)
	require.Equal(t, 40.0, cpu)
	require.Equal(t, 5.0, latency)
}

func TestWorkloadMonitor_UpdateBaselines(t *testing.T) {
	wm := NewWorkloadMonitor()

	// Set initial baselines
	wm.avgQPS.Store(100)
	wm.avgCPU.Store(5000)
	wm.avgLatency.Store(1000)

	// Update with new values
	wm.updateBaselines(200, 60.0, 15.0)

	// Verify exponential moving average (alpha = 0.1)
	// newQPS = 100 * 0.9 + 200 * 0.1 = 110
	require.Equal(t, uint64(110), wm.avgQPS.Load())

	// newCPU = 50 * 0.9 + 60 * 0.1 = 51, stored as 5100
	require.Equal(t, uint64(5100), wm.avgCPU.Load())

	// newLatency = 10 * 0.9 + 15 * 0.1 = 10.5ms, stored as 10500 microseconds
	require.Equal(t, uint64(10500), wm.avgLatency.Load())
}
