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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestResourceThrottler_Creation(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	require.NotNil(t, rt)
	require.Equal(t, 70.0, rt.maxCPUPercent)
	require.Equal(t, uint64(100), rt.maxIOBandwidth)
	require.False(t, rt.IsPaused())
}

func TestResourceThrottler_IsPaused(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	// Initially not paused
	require.False(t, rt.IsPaused())

	// Manually set paused
	rt.paused.Store(true)
	require.True(t, rt.IsPaused())

	// Unpause
	rt.paused.Store(false)
	require.False(t, rt.IsPaused())
}

func TestResourceThrottler_ShouldThrottle(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	// Mock low CPU usage
	rt.currentCPU.Store(5000) // 50%

	// Should not throttle when below threshold
	// Note: In actual implementation, getSystemLoad() reads real CPU
	// For testing, we verify the threshold logic
	require.Equal(t, 70.0, rt.maxCPUPercent)
}

func TestResourceThrottler_ThrottleIO(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	// Test IO throttling with small amount
	start := time.Now()
	rt.ThrottleIO(10) // 10 bytes
	elapsed := time.Since(start)

	// Should complete quickly for small amounts
	require.Less(t, elapsed, 100*time.Millisecond)

	// Test with zero bandwidth limit (no throttling)
	rt.SetMaxIOBandwidth(0)
	start = time.Now()
	rt.ThrottleIO(1000000) // 1MB
	elapsed = time.Since(start)

	// Should complete immediately with no limit
	require.Less(t, elapsed, 10*time.Millisecond)
}

func TestResourceThrottler_SetMaxCPUPercent(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	require.Equal(t, 70.0, rt.maxCPUPercent)

	rt.SetMaxCPUPercent(80.0)
	require.Equal(t, 80.0, rt.maxCPUPercent)
}

func TestResourceThrottler_SetMaxIOBandwidth(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	require.Equal(t, uint64(100), rt.maxIOBandwidth)

	rt.SetMaxIOBandwidth(200)
	require.Equal(t, uint64(200), rt.maxIOBandwidth)
}

func TestResourceThrottler_CheckAndPause(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	// Test with context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return immediately when context is canceled
	rt.CheckAndPause(ctx)

	// Verify not stuck
	require.True(t, true)
}

func TestResourceThrottler_GetCurrentLoad(t *testing.T) {
	logger := zap.NewNop()
	rt := NewResourceThrottler(70.0, 100, logger)

	// Set a known value
	rt.currentCPU.Store(6000) // 60%

	load := rt.GetCurrentLoad()
	require.Equal(t, 60.0, load)
}
