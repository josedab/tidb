// Copyright 2018 PingCAP, Inc.
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

package chunk

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pingcap/tidb/pkg/metrics"
	"github.com/pingcap/tidb/pkg/types"
	"github.com/pingcap/tidb/pkg/util/logutil"
	"github.com/pingcap/tidb/pkg/util/memory"
	"github.com/pingcap/tidb/pkg/util/syncutil"
	"go.uber.org/zap"
)

const (
	// localPoolSize is the maximum number of chunks cached per goroutine
	localPoolSize = 4
	// maxChunkMemorySize is the maximum memory size for a chunk to be pooled (128KB)
	maxChunkMemorySize = 128 * 1024
	// poolAdjustInterval is how often we adjust the adaptive pool size
	poolAdjustInterval = 10 * time.Second
	// memoryPressureThreshold is the memory usage ratio above which we skip pooling
	memoryPressureThreshold = 0.9
	// minPoolSize is the minimum adaptive pool size
	minPoolSize = 100
	// maxPoolSize is the maximum adaptive pool size
	maxPoolSize = 10000
	// targetPoolRatio is the ratio of pooled chunks to active chunks (20%)
	targetPoolRatio = 5
)

var (
	globalChunkPoolMutex syncutil.RWMutex
	// globalChunkPool is a chunk pool, the key is the init capacity.
	globalChunkPool = make(map[int]*Pool)
	// localChunkCache provides per-goroutine chunk caching to reduce contention
	localChunkCache sync.Pool
)

// localChunkPool represents a per-goroutine chunk cache
type localChunkPool struct {
	chunks [localPoolSize]*Chunk
	count  int
}

func init() {
	localChunkCache.New = func() interface{} {
		return &localChunkPool{}
	}
}

// getChunkFromPool gets a Chunk from the Pool. In fact, initCap is the size of the bucket in the histogram.
// so it will not have too many difference value.
// Phase 1: Try local pool first to reduce contention
// Phase 2: Fall back to global pool with adaptive sizing
func getChunkFromPool(initCap int, fields []*types.FieldType) *Chunk {
	// Try local cache first (fast path, no lock contention)
	local := localChunkCache.Get().(*localChunkPool)
	if local.count > 0 {
		local.count--
		chk := local.chunks[local.count]
		local.chunks[local.count] = nil
		localChunkCache.Put(local)

		// Reset and reuse the chunk
		if chk != nil && chk.capacity == initCap && len(chk.columns) == len(fields) {
			metrics.ChunkPoolHits.WithLabelValues("local").Inc()
			chk.Reset()
			return chk
		}
		// Chunk doesn't match, fall through to global pool
	}
	localChunkCache.Put(local)

	// Fall back to global pool
	globalChunkPoolMutex.RLock()
	pool, ok := globalChunkPool[initCap]
	globalChunkPoolMutex.RUnlock()
	if ok {
		chk := pool.GetChunk(fields)
		metrics.ChunkPoolHits.WithLabelValues("global").Inc()
		return chk
	}

	// Create new pool if it doesn't exist
	globalChunkPoolMutex.Lock()
	defer globalChunkPoolMutex.Unlock()
	pool, ok = globalChunkPool[initCap]
	if !ok {
		pool = NewPool(initCap)
		globalChunkPool[initCap] = pool
	}
	chk := pool.GetChunk(fields)
	metrics.ChunkPoolMisses.Inc()
	return chk
}

// putChunkFromPool returns a Chunk to the pool
// Phase 1: Try local pool first for lock-free operation
// Phase 2: Fall back to global pool if local is full
// Phase 3: Check memory pressure before pooling
func putChunkFromPool(initCap int, fields []*types.FieldType, chk *Chunk) {
	// Phase 3: Check if chunk is too large (memory waste prevention)
	memUsage := chk.MemoryUsage()
	if memUsage > maxChunkMemorySize {
		metrics.ChunkPoolDrops.WithLabelValues("size_limit").Inc()
		return
	}

	// Phase 3: Check memory pressure
	memTotal, err := memory.MemTotal()
	if err == nil {
		memUsed, err := memory.MemUsed()
		if err == nil && memTotal > 0 {
			memRatio := float64(memUsed) / float64(memTotal)
			if memRatio > memoryPressureThreshold {
				logutil.BgLogger().Debug("Skipping chunk pooling due to memory pressure",
					zap.Float64("memUsage", memRatio))
				metrics.ChunkPoolDrops.WithLabelValues("memory_pressure").Inc()
				return
			}
		}
	}

	// Phase 1: Try local pool first (fast path)
	local := localChunkCache.Get().(*localChunkPool)
	if local.count < localPoolSize {
		local.chunks[local.count] = chk
		local.count++
		localChunkCache.Put(local)
		return
	}
	localChunkCache.Put(local)

	// Phase 2: Local pool full, return to global pool
	globalChunkPoolMutex.RLock()
	pool, ok := globalChunkPool[initCap]
	globalChunkPoolMutex.RUnlock()
	if ok {
		pool.PutChunk(fields, chk)
		return
	}

	globalChunkPoolMutex.Lock()
	defer globalChunkPoolMutex.Unlock()
	pool, ok = globalChunkPool[initCap]
	if !ok {
		pool = NewPool(initCap)
		globalChunkPool[initCap] = pool
	}
	pool.PutChunk(fields, chk)
}

// Pool is the Column pool with adaptive sizing support.
// NOTE: Pool is non-copyable.
type Pool struct {
	initCap int

	varLenColPool   *sync.Pool
	fixLenColPool4  *sync.Pool
	fixLenColPool8  *sync.Pool
	fixLenColPool16 *sync.Pool
	fixLenColPool40 *sync.Pool

	// Adaptive pool sizing (Phase 2)
	activeChunks int64     // Currently in use
	pooledChunks int64     // Currently pooled
	maxPooled    int64     // Dynamic limit
	mu           sync.Mutex // Protects lastAdjust
	lastAdjust   time.Time // Last time we adjusted pool size
}

// NewPool creates a new Pool with adaptive sizing.
func NewPool(initCap int) *Pool {
	return &Pool{
		initCap:         initCap,
		varLenColPool:   &sync.Pool{New: func() any { return newVarLenColumn(initCap) }},
		fixLenColPool4:  &sync.Pool{New: func() any { return newFixedLenColumn(4, initCap) }},
		fixLenColPool8:  &sync.Pool{New: func() any { return newFixedLenColumn(8, initCap) }},
		fixLenColPool16: &sync.Pool{New: func() any { return newFixedLenColumn(16, initCap) }},
		fixLenColPool40: &sync.Pool{New: func() any { return newFixedLenColumn(40, initCap) }},
		maxPooled:       minPoolSize, // Start with minimum
		lastAdjust:      time.Now(),
	}
}

// GetChunk gets a Chunk from the Pool with adaptive sizing tracking.
func (p *Pool) GetChunk(fields []*types.FieldType) *Chunk {
	atomic.AddInt64(&p.activeChunks, 1)

	chk := new(Chunk)
	chk.capacity = p.initCap
	chk.requiredRows = p.initCap
	chk.columns = make([]*Column, len(fields))
	for i, f := range fields {
		switch elemLen := getFixedLen(f); elemLen {
		case VarElemLen:
			chk.columns[i] = p.varLenColPool.Get().(*Column)
		case 4:
			chk.columns[i] = p.fixLenColPool4.Get().(*Column)
		case 8:
			chk.columns[i] = p.fixLenColPool8.Get().(*Column)
		case 16:
			chk.columns[i] = p.fixLenColPool16.Get().(*Column)
		case 40:
			chk.columns[i] = p.fixLenColPool40.Get().(*Column)
		}
	}

	// Update metrics
	metrics.ChunkPoolSize.WithLabelValues("active").Set(float64(atomic.LoadInt64(&p.activeChunks)))

	return chk
}

// PutChunk puts a Chunk back to the Pool with adaptive sizing.
func (p *Pool) PutChunk(fields []*types.FieldType, chk *Chunk) {
	atomic.AddInt64(&p.activeChunks, -1)

	// Check if pool is over limit (adaptive sizing)
	pooled := atomic.LoadInt64(&p.pooledChunks)
	maxPooled := atomic.LoadInt64(&p.maxPooled)
	if pooled >= maxPooled {
		// Drop chunk instead of pooling
		chk.columns = nil
		metrics.ChunkPoolDrops.WithLabelValues("pool_limit").Inc()
		return
	}

	atomic.AddInt64(&p.pooledChunks, 1)

	for i, f := range fields {
		c := chk.columns[i]
		c.reset()
		switch elemLen := getFixedLen(f); elemLen {
		case VarElemLen:
			p.varLenColPool.Put(c)
		case 4:
			p.fixLenColPool4.Put(c)
		case 8:
			p.fixLenColPool8.Put(c)
		case 16:
			p.fixLenColPool16.Put(c)
		case 40:
			p.fixLenColPool40.Put(c)
		}
	}
	chk.columns = nil // release the Column references.

	// Update metrics
	metrics.ChunkPoolSize.WithLabelValues("pooled").Set(float64(atomic.LoadInt64(&p.pooledChunks)))

	// Periodically adjust pool size
	p.adjustPoolSize()
}

// adjustPoolSize implements Phase 2: adaptive pool sizing
// Adjusts the pool size based on workload patterns
func (p *Pool) adjustPoolSize() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Adjust every 10 seconds
	if time.Since(p.lastAdjust) < poolAdjustInterval {
		return
	}
	p.lastAdjust = time.Now()

	active := atomic.LoadInt64(&p.activeChunks)
	pooled := atomic.LoadInt64(&p.pooledChunks)

	// Heuristic: Keep pool size at 20% of active chunks
	targetPoolSize := active / targetPoolRatio

	// Bounds: min 100, max 10000
	if targetPoolSize < minPoolSize {
		targetPoolSize = minPoolSize
	}
	if targetPoolSize > maxPoolSize {
		targetPoolSize = maxPoolSize
	}

	atomic.StoreInt64(&p.maxPooled, targetPoolSize)

	logutil.BgLogger().Debug("Adjusted chunk pool size",
		zap.Int64("active", active),
		zap.Int64("pooled", pooled),
		zap.Int64("maxPooled", targetPoolSize),
		zap.Int("initCap", p.initCap))
}
