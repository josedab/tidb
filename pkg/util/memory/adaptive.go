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
	"sync/atomic"
)

// AdaptiveThresholdManager calculates adaptive memory thresholds
type AdaptiveThresholdManager struct {
	pressureMonitor *PressureMonitor
	enabled         atomic.Bool

	// Active session tracking
	activeSessions atomic.Int32
}

// NewAdaptiveThresholdManager creates a new manager
func NewAdaptiveThresholdManager(pm *PressureMonitor) *AdaptiveThresholdManager {
	return &AdaptiveThresholdManager{
		pressureMonitor: pm,
	}
}

// Enable turns on adaptive thresholds
func (m *AdaptiveThresholdManager) Enable() {
	m.enabled.Store(true)
}

// Disable turns off adaptive thresholds (fallback to fixed)
func (m *AdaptiveThresholdManager) Disable() {
	m.enabled.Store(false)
}

// IsEnabled returns whether adaptive thresholds are enabled
func (m *AdaptiveThresholdManager) IsEnabled() bool {
	return m.enabled.Load()
}

// RegisterSession increments active session count
func (m *AdaptiveThresholdManager) RegisterSession() {
	m.activeSessions.Add(1)
}

// UnregisterSession decrements active session count
func (m *AdaptiveThresholdManager) UnregisterSession() {
	m.activeSessions.Add(-1)
}

// GetActiveSessionCount returns current active sessions
func (m *AdaptiveThresholdManager) GetActiveSessionCount() int {
	return int(m.activeSessions.Load())
}

// CalculateAdaptiveThreshold computes threshold for a query
func (m *AdaptiveThresholdManager) CalculateAdaptiveThreshold(
	baseThreshold int64,
) int64 {
	// If disabled or monitor unavailable, use base threshold
	if !m.enabled.Load() || m.pressureMonitor == nil || !m.pressureMonitor.IsEnabled() {
		return baseThreshold
	}

	// Get pressure multiplier
	pressureMultiplier := m.pressureMonitor.GetPressureMultiplier()

	// Calculate concurrency factor
	activeSessions := m.GetActiveSessionCount()
	concurrencyFactor := calculateConcurrencyFactor(activeSessions)

	// Calculate adaptive threshold
	adaptiveThreshold := float64(baseThreshold) *
		pressureMultiplier *
		concurrencyFactor

	// Ensure minimum threshold (100MB)
	const minThreshold = 100 * 1024 * 1024
	if adaptiveThreshold < minThreshold {
		adaptiveThreshold = minThreshold
	}

	return int64(adaptiveThreshold)
}

// calculateConcurrencyFactor reduces threshold as concurrency increases
func calculateConcurrencyFactor(activeSessions int) float64 {
	// No reduction for first 10 sessions
	if activeSessions <= 10 {
		return 1.0
	}

	// Reduce by 2% per session beyond 10
	factor := 1.0 - float64(activeSessions-10)*0.02

	// Cap minimum at 0.5
	if factor < 0.5 {
		factor = 0.5
	}

	return factor
}
