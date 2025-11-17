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

package autoanalyze

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSmartPriorityQueue_Creation(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	require.NotNil(t, spq)
	require.Equal(t, 0, spq.Len())
}

func TestSmartPriorityQueue_AddAndPopTask(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Add a task
	task := &AnalysisTask{
		TableID:     1,
		TableName:   "test_table",
		ModifyCount: 1000,
		TotalCount:  2000,
		QueryCount:  100,
		LastAnalyze: time.Now().Add(-24 * time.Hour),
	}

	spq.AddTask(task)
	require.Equal(t, 1, spq.Len())

	// Pop the task
	poppedTask := spq.PopTask()
	require.NotNil(t, poppedTask)
	require.Equal(t, int64(1), poppedTask.TableID)
	require.Equal(t, 0, spq.Len())
}

func TestSmartPriorityQueue_PriorityOrdering(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Add tasks with different priorities
	// Task 1: High modify ratio (50%)
	task1 := &AnalysisTask{
		TableID:     1,
		TableName:   "high_priority",
		ModifyCount: 1000,
		TotalCount:  2000,
		QueryCount:  100,
		LastAnalyze: time.Now().Add(-24 * time.Hour),
	}

	// Task 2: Low modify ratio (1%)
	task2 := &AnalysisTask{
		TableID:     2,
		TableName:   "low_priority",
		ModifyCount: 100,
		TotalCount:  10000,
		QueryCount:  50,
		LastAnalyze: time.Now().Add(-12 * time.Hour),
	}

	// Task 3: Very high modify ratio (90%)
	task3 := &AnalysisTask{
		TableID:     3,
		TableName:   "very_high_priority",
		ModifyCount: 9000,
		TotalCount:  10000,
		QueryCount:  200,
		LastAnalyze: time.Now().Add(-48 * time.Hour),
	}

	spq.AddTask(task1)
	spq.AddTask(task2)
	spq.AddTask(task3)

	require.Equal(t, 3, spq.Len())

	// Task 3 should have highest priority (90% modified, high query count, old stats)
	firstTask := spq.PopTask()
	require.Equal(t, int64(3), firstTask.TableID)

	// Task 1 should be next (50% modified)
	secondTask := spq.PopTask()
	require.Equal(t, int64(1), secondTask.TableID)

	// Task 2 should be last (only 1% modified)
	thirdTask := spq.PopTask()
	require.Equal(t, int64(2), thirdTask.TableID)

	require.Equal(t, 0, spq.Len())
}

func TestSmartPriorityQueue_UpdateTask(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Add initial task
	task := &AnalysisTask{
		TableID:     1,
		TableName:   "test_table",
		ModifyCount: 100,
		TotalCount:  1000,
		QueryCount:  10,
		LastAnalyze: time.Now().Add(-1 * time.Hour),
	}

	spq.AddTask(task)
	initialPriority := spq.PeekTask().Priority

	// Update the same task with higher modification count
	updatedTask := &AnalysisTask{
		TableID:     1,
		TableName:   "test_table",
		ModifyCount: 500,
		TotalCount:  1000,
		QueryCount:  10,
		LastAnalyze: time.Now().Add(-1 * time.Hour),
	}

	spq.AddTask(updatedTask)

	// Should still have only 1 task
	require.Equal(t, 1, spq.Len())

	// Priority should be higher now
	updatedPriority := spq.PeekTask().Priority
	require.Greater(t, updatedPriority, initialPriority)
}

func TestSmartPriorityQueue_UpdateQueryStats(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Add task
	task := &AnalysisTask{
		TableID:     1,
		TableName:   "test_table",
		ModifyCount: 100,
		TotalCount:  1000,
		QueryCount:  10,
		LastAnalyze: time.Now().Add(-1 * time.Hour),
	}

	spq.AddTask(task)
	initialPriority := spq.PeekTask().Priority

	// Update query stats (significantly increase query count)
	spq.UpdateQueryStats(1, 1000)

	// Priority should be recalculated and higher
	updatedPriority := spq.PeekTask().Priority
	require.Greater(t, updatedPriority, initialPriority)

	// Query count should be updated
	require.Equal(t, int64(1000), spq.PeekTask().QueryCount)
}

func TestSmartPriorityQueue_PeekTask(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Empty queue
	require.Nil(t, spq.PeekTask())

	// Add task
	task := &AnalysisTask{
		TableID:     1,
		TableName:   "test_table",
		ModifyCount: 100,
		TotalCount:  1000,
	}

	spq.AddTask(task)

	// Peek should return task without removing it
	peeked := spq.PeekTask()
	require.NotNil(t, peeked)
	require.Equal(t, int64(1), peeked.TableID)
	require.Equal(t, 1, spq.Len())

	// Peek again should return same task
	peeked2 := spq.PeekTask()
	require.NotNil(t, peeked2)
	require.Equal(t, int64(1), peeked2.TableID)
	require.Equal(t, 1, spq.Len())
}

func TestSmartPriorityQueue_Clear(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Add multiple tasks
	for i := 1; i <= 5; i++ {
		task := &AnalysisTask{
			TableID:     int64(i),
			TableName:   "test_table",
			ModifyCount: 100,
			TotalCount:  1000,
		}
		spq.AddTask(task)
	}

	require.Equal(t, 5, spq.Len())

	// Clear all tasks
	spq.Clear()
	require.Equal(t, 0, spq.Len())
	require.Nil(t, spq.PeekTask())
}

func TestSmartPriorityQueue_GetAllTasks(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Add multiple tasks
	for i := 1; i <= 3; i++ {
		task := &AnalysisTask{
			TableID:     int64(i),
			TableName:   "test_table",
			ModifyCount: 100 * int64(i),
			TotalCount:  1000,
		}
		spq.AddTask(task)
	}

	tasks := spq.GetAllTasks()
	require.Equal(t, 3, len(tasks))

	// Verify tasks are copied (modifying returned slice doesn't affect queue)
	tasks[0] = nil
	require.Equal(t, 3, spq.Len())
}

func TestSmartPriorityQueue_CalculatePriority(t *testing.T) {
	logger := zap.NewNop()
	spq := NewSmartPriorityQueue(logger)

	// Test various scenarios
	testCases := []struct {
		name        string
		task        *AnalysisTask
		description string
	}{
		{
			name: "high_staleness",
			task: &AnalysisTask{
				TableID:     1,
				ModifyCount: 800,
				TotalCount:  1000, // 80% modified
				QueryCount:  100,
				LastAnalyze: time.Now().Add(-24 * time.Hour),
			},
			description: "Should have high priority due to staleness",
		},
		{
			name: "high_query_frequency",
			task: &AnalysisTask{
				TableID:     2,
				ModifyCount: 100,
				TotalCount:  1000, // 10% modified
				QueryCount:  10000,
				LastAnalyze: time.Now().Add(-1 * time.Hour),
			},
			description: "Should have high priority due to query frequency",
		},
		{
			name: "small_table",
			task: &AnalysisTask{
				TableID:     3,
				ModifyCount: 10,
				TotalCount:  100, // Small table
				QueryCount:  10,
				LastAnalyze: time.Now().Add(-1 * time.Hour),
			},
			description: "Should have higher priority due to small size (quick win)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set query stats for normalization
			spq.UpdateQueryStats(1, 100)
			spq.UpdateQueryStats(2, 10000)
			spq.UpdateQueryStats(3, 10)

			priority := spq.calculatePriority(tc.task)

			// Priority should be between 0 and 1
			require.GreaterOrEqual(t, priority, 0.0)
			require.LessOrEqual(t, priority, 1.0)

			t.Logf("%s: priority=%.4f (%s)", tc.name, priority, tc.description)
		})
	}
}
