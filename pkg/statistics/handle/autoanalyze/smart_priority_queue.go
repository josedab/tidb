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
	"container/heap"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AnalysisTask represents a table/partition needing analysis
type AnalysisTask struct {
	TableID       int64
	PartitionID   int64 // 0 for full table
	Priority      float64
	ModifyCount   int64
	TotalCount    int64
	QueryCount    int64 // Queries accessing this table (last 24h)
	LastAnalyze   time.Time
	IsIncremental bool   // true for partition-level analysis
	TableName     string // For logging
}

// taskHeap implements heap.Interface for AnalysisTask
type taskHeap []*AnalysisTask

func (h taskHeap) Len() int { return len(h) }

func (h taskHeap) Less(i, j int) bool {
	// Higher priority comes first
	return h[i].Priority > h[j].Priority
}

func (h taskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *taskHeap) Push(x interface{}) {
	task := x.(*AnalysisTask)
	*h = append(*h, task)
}

func (h *taskHeap) Pop() interface{} {
	old := *h
	n := len(old)
	task := old[n-1]
	*h = old[0 : n-1]
	return task
}

// SmartPriorityQueue manages analysis tasks with intelligent prioritization
type SmartPriorityQueue struct {
	mu         sync.Mutex
	tasks      taskHeap
	taskMap    map[int64]*AnalysisTask // tableID -> task (for deduplication)
	queryStats map[int64]int64         // tableID -> query count
	logger     *zap.Logger
}

// NewSmartPriorityQueue creates a smart priority queue
func NewSmartPriorityQueue(logger *zap.Logger) *SmartPriorityQueue {
	if logger == nil {
		logger = zap.NewNop()
	}
	spq := &SmartPriorityQueue{
		tasks:      make(taskHeap, 0, 100),
		taskMap:    make(map[int64]*AnalysisTask),
		queryStats: make(map[int64]int64),
		logger:     logger,
	}
	heap.Init(&spq.tasks)
	return spq
}

// AddTask adds or updates an analysis task
func (spq *SmartPriorityQueue) AddTask(task *AnalysisTask) {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	// Calculate priority
	task.Priority = spq.calculatePriority(task)

	// Check if task already exists
	if existing, ok := spq.taskMap[task.TableID]; ok {
		// Update existing task if new priority is higher
		if task.Priority > existing.Priority {
			existing.Priority = task.Priority
			existing.ModifyCount = task.ModifyCount
			existing.QueryCount = task.QueryCount
			existing.LastAnalyze = task.LastAnalyze
			heap.Fix(&spq.tasks, spq.findTaskIndex(existing))

			spq.logger.Debug("updated task priority",
				zap.String("table", task.TableName),
				zap.Int64("table_id", task.TableID),
				zap.Float64("priority", task.Priority))
		}
	} else {
		// Add new task
		heap.Push(&spq.tasks, task)
		spq.taskMap[task.TableID] = task

		spq.logger.Debug("added new task",
			zap.String("table", task.TableName),
			zap.Int64("table_id", task.TableID),
			zap.Float64("priority", task.Priority))
	}
}

// PopTask returns highest priority task
func (spq *SmartPriorityQueue) PopTask() *AnalysisTask {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	if len(spq.tasks) == 0 {
		return nil
	}

	task := heap.Pop(&spq.tasks).(*AnalysisTask)
	delete(spq.taskMap, task.TableID)

	spq.logger.Info("popped task for analysis",
		zap.String("table", task.TableName),
		zap.Int64("table_id", task.TableID),
		zap.Float64("priority", task.Priority),
		zap.Int64("modify_count", task.ModifyCount),
		zap.Int64("total_count", task.TotalCount))

	return task
}

// PeekTask returns highest priority task without removing it
func (spq *SmartPriorityQueue) PeekTask() *AnalysisTask {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	if len(spq.tasks) == 0 {
		return nil
	}

	return spq.tasks[0]
}

// Len returns the number of tasks in the queue
func (spq *SmartPriorityQueue) Len() int {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	return len(spq.tasks)
}

// calculatePriority computes task priority based on multiple factors
func (spq *SmartPriorityQueue) calculatePriority(task *AnalysisTask) float64 {
	// Factor 1: Staleness (0-1, higher = more stale)
	stalenessScore := 0.0
	if task.TotalCount > 0 {
		stalenessScore = float64(task.ModifyCount) / float64(task.TotalCount)
		if stalenessScore > 1.0 {
			stalenessScore = 1.0
		}
	}

	// Factor 2: Query frequency (0-1, normalized)
	queryFreqScore := 0.0
	maxQueryCount := spq.getMaxQueryCount()
	if maxQueryCount > 0 {
		queryFreqScore = float64(task.QueryCount) / float64(maxQueryCount)
		if queryFreqScore > 1.0 {
			queryFreqScore = 1.0
		}
	}

	// Factor 3: Size (inverse, smaller tables prioritized for faster wins)
	sizeScore := 0.0
	if task.TotalCount > 0 {
		// log scale: smaller tables get higher scores
		// 1K rows = ~1.0, 1M rows = ~0.5, 1B rows = ~0.3
		sizeScore = 1.0 / (1.0 + math.Log10(float64(task.TotalCount))/6.0)
	}

	// Factor 4: Time since last analyze (0-1, 1 week = 1.0)
	timeSinceScore := 0.0
	if !task.LastAnalyze.IsZero() {
		hoursSince := time.Since(task.LastAnalyze).Hours()
		timeSinceScore = hoursSince / 168.0 // 168 hours = 1 week
		if timeSinceScore > 1.0 {
			timeSinceScore = 1.0
		}
	}

	// Weighted combination
	// Staleness is most important (40%), then query frequency (30%),
	// then size (20%), and finally time since last analyze (10%)
	priority := (stalenessScore * 0.4) +
		(queryFreqScore * 0.3) +
		(sizeScore * 0.2) +
		(timeSinceScore * 0.1)

	return priority
}

// UpdateQueryStats updates query frequency stats for a table
func (spq *SmartPriorityQueue) UpdateQueryStats(tableID int64, queryCount int64) {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	spq.queryStats[tableID] = queryCount

	// Recalculate priority if task exists
	if task, ok := spq.taskMap[tableID]; ok {
		oldPriority := task.Priority
		task.QueryCount = queryCount
		task.Priority = spq.calculatePriority(task)

		// Only fix heap if priority changed significantly
		if math.Abs(task.Priority-oldPriority) > 0.01 {
			heap.Fix(&spq.tasks, spq.findTaskIndex(task))

			spq.logger.Debug("updated query stats and priority",
				zap.Int64("table_id", tableID),
				zap.Int64("query_count", queryCount),
				zap.Float64("old_priority", oldPriority),
				zap.Float64("new_priority", task.Priority))
		}
	}
}

// getMaxQueryCount returns max query count across all tables
func (spq *SmartPriorityQueue) getMaxQueryCount() int64 {
	var max int64
	for _, count := range spq.queryStats {
		if count > max {
			max = count
		}
	}
	// Return at least 1 to avoid division by zero
	if max == 0 {
		return 1
	}
	return max
}

// findTaskIndex finds task index in heap
func (spq *SmartPriorityQueue) findTaskIndex(task *AnalysisTask) int {
	for i, t := range spq.tasks {
		if t == task {
			return i
		}
	}
	return -1
}

// Clear removes all tasks from the queue
func (spq *SmartPriorityQueue) Clear() {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	spq.tasks = make(taskHeap, 0, 100)
	spq.taskMap = make(map[int64]*AnalysisTask)
	heap.Init(&spq.tasks)
}

// GetAllTasks returns a copy of all tasks (for monitoring/debugging)
func (spq *SmartPriorityQueue) GetAllTasks() []*AnalysisTask {
	spq.mu.Lock()
	defer spq.mu.Unlock()

	tasks := make([]*AnalysisTask, len(spq.tasks))
	copy(tasks, spq.tasks)
	return tasks
}
