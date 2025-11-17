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
	"sync/atomic"
	"time"

	"github.com/pingcap/tidb/pkg/metrics"
	"github.com/pingcap/tidb/pkg/util/cpu"
)

// WorkloadMonitor tracks system load for adaptive scheduling
type WorkloadMonitor struct {
	// Rolling windows (last 12 windows = 1 hour at 5min intervals)
	qpsWindows     [12]uint64  // Queries per second
	cpuWindows     [12]float64 // CPU percentage
	latencyWindows [12]float64 // P99 latency in ms

	currentWindow int
	lastUpdate    time.Time

	// Baselines (learned over 24 hours)
	avgQPS     atomic.Uint64
	avgCPU     atomic.Uint64 // stored as uint64 (percentage * 100)
	avgLatency atomic.Uint64 // stored in microseconds
}

// NewWorkloadMonitor creates a workload monitor
func NewWorkloadMonitor() *WorkloadMonitor {
	wm := &WorkloadMonitor{
		lastUpdate: time.Now(),
	}
	// Initialize with reasonable defaults
	wm.avgQPS.Store(100)                // 100 QPS baseline
	wm.avgCPU.Store(5000)               // 50% CPU baseline
	wm.avgLatency.Store(10000)          // 10ms baseline
	return wm
}

// Update updates workload metrics (called every 5 minutes)
func (wm *WorkloadMonitor) Update() {
	now := time.Now()
	if now.Sub(wm.lastUpdate) < 5*time.Minute {
		return
	}

	// Get current metrics
	qps := wm.getCurrentQPS()
	cpuUsage := wm.getCurrentCPU()
	latency := wm.getCurrentP99Latency()

	// Rotate window
	wm.currentWindow = (wm.currentWindow + 1) % 12
	wm.qpsWindows[wm.currentWindow] = qps
	wm.cpuWindows[wm.currentWindow] = cpuUsage
	wm.latencyWindows[wm.currentWindow] = latency

	wm.lastUpdate = now

	// Update baselines (exponential moving average)
	wm.updateBaselines(qps, cpuUsage, latency)
}

// IsLowTrafficPeriod returns true if current load is low
func (wm *WorkloadMonitor) IsLowTrafficPeriod() bool {
	currentQPS := wm.getCurrentQPS()
	currentCPU := wm.getCurrentCPU()
	currentLatency := wm.getCurrentP99Latency()

	avgQPS := wm.avgQPS.Load()
	avgCPU := float64(wm.avgCPU.Load()) / 100.0
	avgLatency := float64(wm.avgLatency.Load()) / 1000.0 // convert to ms

	// Low traffic criteria:
	// 1. QPS < 50% of average
	// 2. CPU < 50%
	// 3. P99 latency < 2x average
	return currentQPS < avgQPS/2 &&
		currentCPU < 50.0 &&
		currentLatency < avgLatency*2.0
}

// GetLoadFactor returns current load factor (0.0 = idle, 1.0 = peak)
func (wm *WorkloadMonitor) GetLoadFactor() float64 {
	avgQPSVal := wm.avgQPS.Load()
	if avgQPSVal == 0 {
		return 0.0
	}

	avgLatencyVal := float64(wm.avgLatency.Load()) / 1000.0
	if avgLatencyVal == 0 {
		avgLatencyVal = 1.0
	}

	// Weighted combination of QPS, CPU, latency
	qpsFactor := float64(wm.getCurrentQPS()) / float64(avgQPSVal)
	cpuFactor := wm.getCurrentCPU() / 100.0
	latencyFactor := wm.getCurrentP99Latency() / avgLatencyVal

	loadFactor := (qpsFactor*0.4 + cpuFactor*0.4 + latencyFactor*0.2)

	// Clamp to [0.0, 1.0]
	if loadFactor > 1.0 {
		loadFactor = 1.0
	}
	if loadFactor < 0.0 {
		loadFactor = 0.0
	}

	return loadFactor
}

// getCurrentQPS reads current QPS from metrics
func (wm *WorkloadMonitor) getCurrentQPS() uint64 {
	// Get total query count from metrics
	// This is a simplified implementation - in production, would read from actual metrics
	return 100 // Placeholder - will be replaced with actual metrics reading
}

// getCurrentCPU reads current CPU usage
func (wm *WorkloadMonitor) getCurrentCPU() float64 {
	// Use the existing CPU monitoring utility
	cpuUsage := cpu.GetCPUUsage()
	return cpuUsage * 100.0 // Convert to percentage
}

// getCurrentP99Latency reads P99 query latency in milliseconds
func (wm *WorkloadMonitor) getCurrentP99Latency() float64 {
	// This is a simplified implementation - in production, would read from actual metrics
	// Would need to access query duration histogram and calculate P99
	return 10.0 // Placeholder - 10ms default
}

// updateBaselines updates learned baselines using EMA (Exponential Moving Average)
func (wm *WorkloadMonitor) updateBaselines(qps uint64, cpuUsage, latency float64) {
	alpha := 0.1 // EMA smoothing factor

	// Update QPS baseline
	oldQPS := wm.avgQPS.Load()
	newQPS := uint64(float64(oldQPS)*(1-alpha) + float64(qps)*alpha)
	wm.avgQPS.Store(newQPS)

	// Update CPU baseline
	oldCPU := float64(wm.avgCPU.Load()) / 100.0
	newCPU := uint64((oldCPU*(1-alpha) + cpuUsage*alpha) * 100)
	wm.avgCPU.Store(newCPU)

	// Update latency baseline
	oldLatency := float64(wm.avgLatency.Load()) / 1000.0
	newLatency := uint64((oldLatency*(1-alpha) + latency*alpha) * 1000)
	wm.avgLatency.Store(newLatency)
}

// GetCurrentMetrics returns current workload metrics for monitoring
func (wm *WorkloadMonitor) GetCurrentMetrics() (qps uint64, cpu float64, latency float64) {
	return wm.getCurrentQPS(), wm.getCurrentCPU(), wm.getCurrentP99Latency()
}

// GetBaselineMetrics returns baseline metrics
func (wm *WorkloadMonitor) GetBaselineMetrics() (qps uint64, cpu float64, latency float64) {
	return wm.avgQPS.Load(),
		float64(wm.avgCPU.Load()) / 100.0,
		float64(wm.avgLatency.Load()) / 1000.0
}
