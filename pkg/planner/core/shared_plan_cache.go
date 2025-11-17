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

package core

import (
	"sync"
	"time"

	"github.com/pingcap/tidb/pkg/domain"
	"github.com/pingcap/tidb/pkg/planner/core/base"
	core_metrics "github.com/pingcap/tidb/pkg/planner/core/metrics"
	"github.com/pingcap/tidb/pkg/sessionctx"
	"github.com/pingcap/tidb/pkg/types"
	"github.com/pingcap/tidb/pkg/util/kvcache"
	"go.uber.org/atomic"
)

func init() {
	domain.NewSharedPlanCache = func(capacity uint) domain.SharedPlanCache {
		return NewSharedPlanCache(capacity)
	}
}

// CacheKey represents the key for shared plan cache
type CacheKey struct {
	SQLText       string // Normalized SQL text
	SchemaVersion int64  // Schema version (for invalidation)
	DatabaseName  string // Current database
}

// Hash implements the kvcache.Key interface
func (ck CacheKey) Hash() []byte {
	// Simple hash implementation - concatenate all fields
	hash := ck.SQLText + "|" + string(rune(ck.SchemaVersion)) + "|" + ck.DatabaseName
	return []byte(hash)
}

// SharedPlanCacheValue represents a cached plan that can be shared across sessions
type SharedPlanCacheValue struct {
	Plan        base.Plan          // The compiled physical plan
	ParamTypes  []*types.FieldType // Expected parameter types
	OutputNames types.NameSlice    // Output column names

	// Reference counting for LRU eviction
	refCount atomic.Int32 // Number of sessions using this plan

	// Metadata
	CreatedAt time.Time     // When the plan was created
	LastUsed  atomic.Int64  // Unix timestamp of last use
}

// Clone creates a session-local copy for parameter binding
func (spv *SharedPlanCacheValue) Clone() base.Plan {
	// For the shared cache, we need to clone the plan to avoid
	// concurrent modification issues during parameter binding
	cloned, ok := spv.Plan.CloneForPlanCache(nil)
	if !ok {
		return nil
	}
	return cloned
}

// Release decrements reference count
func (spv *SharedPlanCacheValue) Release() {
	spv.refCount.Add(-1)
}

// CanEvict returns true if plan can be evicted (refCount == 0)
func (spv *SharedPlanCacheValue) CanEvict() bool {
	return spv.refCount.Load() == 0
}

// MemoryUsage returns estimated memory usage of this cached value
func (spv *SharedPlanCacheValue) MemoryUsage() int64 {
	// Rough estimation - in production, this should be more accurate
	// For now, return a constant value
	return 10 * 1024 // 10KB per plan
}

// SharedPlanCache is a global plan cache shared across all sessions
type SharedPlanCache struct {
	cache *kvcache.SimpleLRUCache
	mu    sync.RWMutex // Protect concurrent access

	// Metrics
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

// NewSharedPlanCache creates a shared plan cache with the given capacity
func NewSharedPlanCache(capacity uint) *SharedPlanCache {
	spc := &SharedPlanCache{
		cache: kvcache.NewSimpleLRUCache(capacity, 0, 0),
	}
	// Set up eviction callback to handle reference counting
	spc.cache.SetOnEvict(func(key kvcache.Key, value kvcache.Value) {
		if spv, ok := value.(*SharedPlanCacheValue); ok {
			spv.Release()
		}
		spc.evictions.Add(1)
		core_metrics.GetSharedPlanCacheEvictionCounter().Inc()
	})
	return spc
}

// Get retrieves a plan from shared cache
func (spc *SharedPlanCache) Get(key CacheKey, _ sessionctx.Context) (*SharedPlanCacheValue, bool) {
	spc.mu.RLock()
	defer spc.mu.RUnlock()

	value, ok := spc.cache.Get(key)
	if !ok {
		spc.misses.Add(1)
		core_metrics.GetSharedPlanCacheMissCounter().Inc()
		return nil, false
	}

	spc.hits.Add(1)
	core_metrics.GetSharedPlanCacheHitCounter().Inc()
	planValue := value.(*SharedPlanCacheValue)

	// Increment reference count
	planValue.refCount.Add(1)

	// Update last used time
	planValue.LastUsed.Store(time.Now().Unix())

	return planValue, true
}

// Put stores a plan in shared cache
func (spc *SharedPlanCache) Put(key CacheKey, value *SharedPlanCacheValue) {
	spc.mu.Lock()
	defer spc.mu.Unlock()

	// Check if plan already exists (race condition)
	if existing, ok := spc.cache.Get(key); ok {
		// Use existing plan
		existing.(*SharedPlanCacheValue).refCount.Add(1)
		return
	}

	// Insert new plan
	spc.cache.Put(key, value)
}

// Delete removes a plan from cache
func (spc *SharedPlanCache) Delete(key CacheKey) {
	spc.mu.Lock()
	defer spc.mu.Unlock()

	if value, ok := spc.cache.Get(key); ok {
		if spv, ok := value.(*SharedPlanCacheValue); ok {
			spv.Release()
		}
	}
	spc.cache.Delete(key)
	spc.evictions.Add(1)
}

// InvalidateSchema invalidates all plans for a schema version
func (spc *SharedPlanCache) InvalidateSchema(_ int64) {
	spc.mu.Lock()
	defer spc.mu.Unlock()

	// For simplicity, clear all plans when schema changes
	// In production, we would iterate and remove only affected plans
	spc.cache.DeleteAll()
}

// GetStats returns cache statistics
func (spc *SharedPlanCache) GetStats() (hits, misses, evictions uint64) {
	return spc.hits.Load(), spc.misses.Load(), spc.evictions.Load()
}

// Size returns the number of cached plans
func (spc *SharedPlanCache) Size() int {
	spc.mu.RLock()
	defer spc.mu.RUnlock()

	// This is a simple implementation
	// The SimpleLRUCache doesn't expose size, so we'll return 0 for now
	return 0
}

// MemUsage returns the total memory usage of cached plans
func (spc *SharedPlanCache) MemUsage() int64 {
	spc.mu.RLock()
	defer spc.mu.RUnlock()

	// This would need to iterate over all cached values and sum their memory usage
	// For now, return 0
	return 0
}

// generateCacheKey generates a cache key from SQL text and context
func generateCacheKey(sql string, ctx sessionctx.Context) CacheKey {
	return CacheKey{
		SQLText:       normalizeSQLText(sql),
		SchemaVersion: ctx.GetInfoSchema().SchemaMetaVersion(),
		DatabaseName:  ctx.GetSessionVars().CurrentDB,
	}
}

// normalizeSQLText normalizes SQL text by removing comments and extra whitespace
func normalizeSQLText(sql string) string {
	// Simple normalization - in production, this should be more sophisticated
	// For now, just return the original SQL
	return sql
}

// TryGetFromSharedCache attempts to retrieve a plan from the shared cache for a prepared statement.
// This function demonstrates how the shared cache would be integrated into the plan cache lookup flow.
// Returns (plan, hit) where hit indicates whether the plan was found in the shared cache.
func TryGetFromSharedCache(ctx sessionctx.Context, stmt *PlanCacheStmt) (base.Plan, bool) {
	// Get the domain's shared plan cache
	sharedCache := ctx.GetDomainCacheKey()
	if sharedCache == nil {
		return nil, false
	}

	// Generate cache key from statement
	cacheKey := generateCacheKey(stmt.PreparedAst.Stmt.Text(), ctx)

	// Try to get from shared cache
	if cachedValue, hit := sharedCache.(*SharedPlanCache).Get(cacheKey, ctx); hit {
		// Clone the plan for this session
		if clonedPlan := cachedValue.Clone(); clonedPlan != nil {
			// Release the reference after cloning
			// In a full implementation, this would be managed by the execution lifecycle
			defer cachedValue.Release()
			return clonedPlan, true
		}
	}

	return nil, false
}

// PutIntoSharedCache stores a plan into the shared cache for a prepared statement.
// This function demonstrates how a newly compiled plan would be added to the shared cache.
func PutIntoSharedCache(ctx sessionctx.Context, stmt *PlanCacheStmt, plan base.Plan, paramTypes []*types.FieldType) {
	// Get the domain's shared plan cache
	sharedCache := ctx.GetDomainCacheKey()
	if sharedCache == nil {
		return
	}

	// Generate cache key from statement
	cacheKey := generateCacheKey(stmt.PreparedAst.Stmt.Text(), ctx)

	// Create a new shared plan cache value
	sharedValue := &SharedPlanCacheValue{
		Plan:        plan,
		ParamTypes:  paramTypes,
		OutputNames: stmt.OutputNames,
		CreatedAt:   time.Now(),
	}
	sharedValue.refCount.Store(1)
	sharedValue.LastUsed.Store(time.Now().Unix())

	// Put into shared cache
	sharedCache.(*SharedPlanCache).Put(cacheKey, sharedValue)
}
