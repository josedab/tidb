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

package memory

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"go.uber.org/zap"
)

// PressureLevel represents memory pressure state
type PressureLevel int32

const (
	// PressureLevelGreen indicates > 50% available memory
	PressureLevelGreen PressureLevel = 0
	// PressureLevelYellow indicates 25-50% available memory
	PressureLevelYellow PressureLevel = 1
	// PressureLevelOrange indicates 10-25% available memory
	PressureLevelOrange PressureLevel = 2
	// PressureLevelRed indicates < 10% available memory
	PressureLevelRed PressureLevel = 3
)

// String returns the string representation of the pressure level
func (p PressureLevel) String() string {
	switch p {
	case PressureLevelGreen:
		return "GREEN"
	case PressureLevelYellow:
		return "YELLOW"
	case PressureLevelOrange:
		return "ORANGE"
	case PressureLevelRed:
		return "RED"
	default:
		return "UNKNOWN"
	}
}

// PressureMonitor monitors system memory pressure
type PressureMonitor struct {
	currentLevel   atomic.Int32
	enabled        atomic.Bool
	pollInterval   time.Duration
	logger         *zap.Logger

	// Metrics
	lastAvailableBytes atomic.Uint64
	lastTotalBytes     atomic.Uint64
}

// NewPressureMonitor creates a new monitor
func NewPressureMonitor(logger *zap.Logger) *PressureMonitor {
	return &PressureMonitor{
		pollInterval: time.Second,
		logger:       logger,
	}
}

// Start begins monitoring (called at server startup)
func (pm *PressureMonitor) Start(ctx context.Context) {
	pm.enabled.Store(true)
	pm.logger.Info("starting memory pressure monitor",
		zap.Duration("poll_interval", pm.pollInterval))

	go pm.monitorLoop(ctx)
}

// monitorLoop polls memory stats periodically
func (pm *PressureMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(pm.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			pm.logger.Info("stopping memory pressure monitor")
			pm.enabled.Store(false)
			return
		case <-ticker.C:
			pm.updatePressureLevel()
		}
	}
}

// updatePressureLevel reads system memory and updates pressure level
func (pm *PressureMonitor) updatePressureLevel() {
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		pm.logger.Warn("failed to read memory stats", zap.Error(err))
		return
	}

	// Update metrics
	pm.lastAvailableBytes.Store(vmStat.Available)
	pm.lastTotalBytes.Store(vmStat.Total)

	// Calculate available percentage
	availablePercent := float64(vmStat.Available) / float64(vmStat.Total) * 100

	// Determine pressure level
	var newLevel PressureLevel
	switch {
	case availablePercent > 50:
		newLevel = PressureLevelGreen
	case availablePercent > 25:
		newLevel = PressureLevelYellow
	case availablePercent > 10:
		newLevel = PressureLevelOrange
	default:
		newLevel = PressureLevelRed
	}

	oldLevel := PressureLevel(pm.currentLevel.Swap(int32(newLevel)))

	// Log level changes
	if oldLevel != newLevel {
		pm.logger.Info("memory pressure level changed",
			zap.String("old_level", oldLevel.String()),
			zap.String("new_level", newLevel.String()),
			zap.Float64("available_percent", availablePercent),
			zap.Uint64("available_bytes", vmStat.Available),
			zap.Uint64("total_bytes", vmStat.Total))
	}
}

// GetPressureLevel returns current pressure level
func (pm *PressureMonitor) GetPressureLevel() PressureLevel {
	return PressureLevel(pm.currentLevel.Load())
}

// GetPressureMultiplier returns threshold multiplier for current pressure
func (pm *PressureMonitor) GetPressureMultiplier() float64 {
	level := pm.GetPressureLevel()

	switch level {
	case PressureLevelGreen:
		return 2.0 // Allow 2x larger operations
	case PressureLevelYellow:
		return 1.0 // Default behavior
	case PressureLevelOrange:
		return 0.5 // Spill earlier
	case PressureLevelRed:
		return 0.25 // Aggressive spilling
	default:
		return 1.0
	}
}

// IsEnabled returns whether monitoring is enabled
func (pm *PressureMonitor) IsEnabled() bool {
	return pm.enabled.Load()
}

// GetStats returns current memory statistics
func (pm *PressureMonitor) GetStats() (available, total uint64) {
	return pm.lastAvailableBytes.Load(), pm.lastTotalBytes.Load()
}

// SetPollInterval updates the polling interval (for testing)
func (pm *PressureMonitor) SetPollInterval(interval time.Duration) {
	pm.pollInterval = interval
}
