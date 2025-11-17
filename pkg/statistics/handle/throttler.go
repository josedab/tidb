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
	"sync/atomic"
	"time"

	"github.com/pingcap/tidb/pkg/util/cpu"
	"go.uber.org/zap"
)

// ResourceThrottler limits stats collection impact on system
type ResourceThrottler struct {
	maxCPUPercent  float64 // Max CPU% for stats collection
	maxIOBandwidth uint64  // Max IO bandwidth (bytes/sec)

	currentCPU atomic.Uint64 // Current CPU usage (* 100)
	paused     atomic.Bool   // Is stats collection paused?

	logger *zap.Logger
}

// NewResourceThrottler creates a throttler
func NewResourceThrottler(maxCPU float64, maxIOBW uint64, logger *zap.Logger) *ResourceThrottler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ResourceThrottler{
		maxCPUPercent:  maxCPU,
		maxIOBandwidth: maxIOBW,
		logger:         logger,
	}
}

// CheckAndPause checks system load and pauses if necessary
func (rt *ResourceThrottler) CheckAndPause(ctx context.Context) {
	currentLoad := rt.getSystemLoad()

	if currentLoad > rt.maxCPUPercent {
		if !rt.paused.Load() {
			rt.logger.Info("pausing stats collection due to high system load",
				zap.Float64("current_load", currentLoad),
				zap.Float64("threshold", rt.maxCPUPercent))
			rt.paused.Store(true)
		}

		// Wait until load decreases
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentLoad = rt.getSystemLoad()
				if currentLoad <= rt.maxCPUPercent*0.8 { // 20% hysteresis
					rt.logger.Info("resuming stats collection",
						zap.Float64("current_load", currentLoad))
					rt.paused.Store(false)
					return
				}
			}
		}
	}
}

// IsPaused returns whether stats collection is paused
func (rt *ResourceThrottler) IsPaused() bool {
	return rt.paused.Load()
}

// ShouldThrottle returns true if the system load is too high
func (rt *ResourceThrottler) ShouldThrottle() bool {
	currentLoad := rt.getSystemLoad()
	return currentLoad > rt.maxCPUPercent
}

// ThrottleIO rate-limits IO operations (simplified implementation)
func (rt *ResourceThrottler) ThrottleIO(bytesRead uint64) {
	// Sleep to maintain target bandwidth
	if rt.maxIOBandwidth == 0 {
		return
	}

	// Calculate required sleep time to maintain bandwidth limit
	sleepDuration := time.Duration(float64(bytesRead) / float64(rt.maxIOBandwidth) * float64(time.Second))
	if sleepDuration > 0 {
		time.Sleep(sleepDuration)
	}
}

// getSystemLoad returns current system CPU load
func (rt *ResourceThrottler) getSystemLoad() float64 {
	// Use the existing CPU monitoring utility
	cpuUsage := cpu.GetCPUUsage()
	loadPercent := cpuUsage * 100.0

	// Store for monitoring
	rt.currentCPU.Store(uint64(loadPercent * 100))

	return loadPercent
}

// GetCurrentLoad returns the current CPU load percentage
func (rt *ResourceThrottler) GetCurrentLoad() float64 {
	return float64(rt.currentCPU.Load()) / 100.0
}

// SetMaxCPUPercent updates the maximum CPU percentage threshold
func (rt *ResourceThrottler) SetMaxCPUPercent(maxCPU float64) {
	rt.maxCPUPercent = maxCPU
}

// SetMaxIOBandwidth updates the maximum IO bandwidth
func (rt *ResourceThrottler) SetMaxIOBandwidth(maxIO uint64) {
	rt.maxIOBandwidth = maxIO
}
