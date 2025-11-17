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
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSharedPlanCache_BasicOperations tests basic cache operations
func TestSharedPlanCache_BasicOperations(t *testing.T) {
	cache := NewSharedPlanCache(10)
	require.NotNil(t, cache)

	// Test cache miss
	key := CacheKey{
		SQLText:       "SELECT * FROM t WHERE id = ?",
		SchemaVersion: 1,
		DatabaseName:  "test",
	}
	_, ok := cache.Get(key, nil)
	require.False(t, ok, "Cache should miss on first access")

	// Test cache put and get
	value := &SharedPlanCacheValue{
		Plan:       nil, // mock plan
		ParamTypes: nil,
	}
	value.refCount.Store(1)
	cache.Put(key, value)

	// Now get should hit
	cached, ok := cache.Get(key, nil)
	require.True(t, ok, "Cache should hit after put")
	require.NotNil(t, cached)

	// Check that reference count was incremented
	require.Equal(t, int32(2), cached.refCount.Load(), "Reference count should be incremented on Get")

	// Test statistics
	hits, misses, _ := cache.GetStats()
	require.Equal(t, uint64(1), hits, "Should have 1 hit")
	require.Equal(t, uint64(1), misses, "Should have 1 miss")
}

// TestSharedPlanCache_ReferenceCount tests reference counting
func TestSharedPlanCache_ReferenceCount(t *testing.T) {
	cache := NewSharedPlanCache(10)

	key := CacheKey{
		SQLText:       "SELECT * FROM users WHERE id = ?",
		SchemaVersion: 1,
		DatabaseName:  "test",
	}

	value := &SharedPlanCacheValue{
		Plan:       nil,
		ParamTypes: nil,
	}
	value.refCount.Store(1)

	cache.Put(key, value)

	// Get multiple times
	cached1, ok := cache.Get(key, nil)
	require.True(t, ok)
	require.Equal(t, int32(2), cached1.refCount.Load())

	cached2, ok := cache.Get(key, nil)
	require.True(t, ok)
	require.Equal(t, int32(3), cached2.refCount.Load())

	// Cannot evict while refCount > 0
	require.False(t, cached2.CanEvict())

	// Release references
	cached1.Release()
	require.Equal(t, int32(2), cached1.refCount.Load())

	cached2.Release()
	require.Equal(t, int32(1), cached2.refCount.Load())

	cached2.Release()
	require.Equal(t, int32(0), cached2.refCount.Load())

	// Now can evict
	require.True(t, cached2.CanEvict())
}

// TestSharedPlanCache_ConcurrentAccess tests concurrent access to the cache
func TestSharedPlanCache_ConcurrentAccess(t *testing.T) {
	cache := NewSharedPlanCache(100)

	// Spawn 100 goroutines accessing cache concurrently
	var wg sync.WaitGroup
	numGoroutines := 100
	numKeys := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine accesses a subset of keys
			key := CacheKey{
				SQLText:       fmt.Sprintf("SELECT * FROM t WHERE id = %d", id%numKeys),
				SchemaVersion: 1,
				DatabaseName:  "test",
			}

			// Try to get from cache
			if _, hit := cache.Get(key, nil); !hit {
				// Cache miss, create and put
				value := &SharedPlanCacheValue{
					Plan:       nil,
					ParamTypes: nil,
				}
				value.refCount.Store(1)
				cache.Put(key, value)
			}
		}(i)
	}

	wg.Wait()

	// Verify no race conditions occurred
	// The exact counts depend on timing, but we should have some hits and misses
	hits, misses, _ := cache.GetStats()
	t.Logf("Concurrent access stats: hits=%d, misses=%d", hits, misses)
	require.Greater(t, hits+misses, uint64(0), "Should have some cache activity")
}

// TestSharedPlanCache_Eviction tests cache eviction
func TestSharedPlanCache_Eviction(t *testing.T) {
	// Create a small cache that will trigger eviction
	cache := NewSharedPlanCache(2)

	// Add 3 entries to trigger eviction
	for i := 0; i < 3; i++ {
		key := CacheKey{
			SQLText:       fmt.Sprintf("SELECT * FROM t%d", i),
			SchemaVersion: 1,
			DatabaseName:  "test",
		}
		value := &SharedPlanCacheValue{
			Plan:       nil,
			ParamTypes: nil,
		}
		value.refCount.Store(1)
		cache.Put(key, value)
	}

	// Check eviction stats
	_, _, evictions := cache.GetStats()
	require.Greater(t, evictions, uint64(0), "Should have at least one eviction")
}

// TestSharedPlanCache_InvalidateSchema tests schema invalidation
func TestSharedPlanCache_InvalidateSchema(t *testing.T) {
	cache := NewSharedPlanCache(10)

	// Add some entries
	for i := 0; i < 5; i++ {
		key := CacheKey{
			SQLText:       fmt.Sprintf("SELECT * FROM t%d", i),
			SchemaVersion: 1,
			DatabaseName:  "test",
		}
		value := &SharedPlanCacheValue{
			Plan:       nil,
			ParamTypes: nil,
		}
		value.refCount.Store(1)
		cache.Put(key, value)
	}

	// Verify we can get entries
	key := CacheKey{
		SQLText:       "SELECT * FROM t0",
		SchemaVersion: 1,
		DatabaseName:  "test",
	}
	_, ok := cache.Get(key, nil)
	require.True(t, ok, "Should find entry before invalidation")

	// Invalidate schema
	cache.InvalidateSchema(1)

	// After invalidation, cache should be empty
	_, ok = cache.Get(key, nil)
	require.False(t, ok, "Should not find entry after schema invalidation")
}

// TestCacheKey_Hash tests cache key hashing
func TestCacheKey_Hash(t *testing.T) {
	key1 := CacheKey{
		SQLText:       "SELECT * FROM t WHERE id = ?",
		SchemaVersion: 1,
		DatabaseName:  "test",
	}

	key2 := CacheKey{
		SQLText:       "SELECT * FROM t WHERE id = ?",
		SchemaVersion: 1,
		DatabaseName:  "test",
	}

	key3 := CacheKey{
		SQLText:       "SELECT * FROM t WHERE id = ?",
		SchemaVersion: 2, // different schema version
		DatabaseName:  "test",
	}

	// Same keys should produce same hash
	require.Equal(t, string(key1.Hash()), string(key2.Hash()))

	// Different schema versions should produce different hashes
	require.NotEqual(t, string(key1.Hash()), string(key3.Hash()))
}

// TestSharedPlanCacheValue_MemoryUsage tests memory usage estimation
func TestSharedPlanCacheValue_MemoryUsage(t *testing.T) {
	value := &SharedPlanCacheValue{
		Plan:       nil,
		ParamTypes: nil,
	}

	mem := value.MemoryUsage()
	require.Greater(t, mem, int64(0), "Memory usage should be positive")
}
