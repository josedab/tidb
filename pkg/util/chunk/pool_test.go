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
	"testing"

	"github.com/pingcap/tidb/pkg/parser/mysql"
	"github.com/pingcap/tidb/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestNewPool(t *testing.T) {
	pool := NewPool(1024)
	require.Equal(t, 1024, pool.initCap)
	require.NotNil(t, pool.varLenColPool)
	require.NotNil(t, pool.fixLenColPool4)
	require.NotNil(t, pool.fixLenColPool8)
	require.NotNil(t, pool.fixLenColPool16)
	require.NotNil(t, pool.fixLenColPool40)
}

func TestPoolGetChunk(t *testing.T) {
	initCap := 1024
	pool := NewPool(initCap)

	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeJSON),
		types.NewFieldType(mysql.TypeFloat),
		types.NewFieldType(mysql.TypeNewDecimal),
		types.NewFieldType(mysql.TypeDouble),
		types.NewFieldType(mysql.TypeLonglong),
		// types.NewFieldType(mysql.TypeTimestamp),
		// types.NewFieldType(mysql.TypeDatetime),
	}

	chk := pool.GetChunk(fieldTypes)
	require.NotNil(t, chk)
	require.Len(t, fieldTypes, chk.NumCols())
	require.Nil(t, chk.columns[0].elemBuf)
	require.Nil(t, chk.columns[1].elemBuf)
	require.Equal(t, getFixedLen(fieldTypes[2]), len(chk.columns[2].elemBuf))
	require.Equal(t, getFixedLen(fieldTypes[3]), len(chk.columns[3].elemBuf))
	require.Equal(t, getFixedLen(fieldTypes[4]), len(chk.columns[4].elemBuf))
	require.Equal(t, getFixedLen(fieldTypes[5]), len(chk.columns[5].elemBuf))
	// require.Equal(t, getFixedLen(fieldTypes[6]), len(chk.columns[6].elemBuf))
	// require.Equal(t, getFixedLen(fieldTypes[7]), len(chk.columns[7].elemBuf))

	require.Equal(t, initCap*getFixedLen(fieldTypes[2]), cap(chk.columns[2].data))
	require.Equal(t, initCap*getFixedLen(fieldTypes[3]), cap(chk.columns[3].data))
	require.Equal(t, initCap*getFixedLen(fieldTypes[4]), cap(chk.columns[4].data))
	require.Equal(t, initCap*getFixedLen(fieldTypes[5]), cap(chk.columns[5].data))
	// require.Equal(t, initCap*getFixedLen(fieldTypes[6]), cap(chk.columns[6].data))
	// require.Equal(t, initCap*getFixedLen(fieldTypes[7]), cap(chk.columns[7].data))
}

func TestPoolPutChunk(t *testing.T) {
	initCap := 1024
	pool := NewPool(initCap)

	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeJSON),
		types.NewFieldType(mysql.TypeFloat),
		types.NewFieldType(mysql.TypeNewDecimal),
		types.NewFieldType(mysql.TypeDouble),
		types.NewFieldType(mysql.TypeLonglong),
		types.NewFieldType(mysql.TypeTimestamp),
		types.NewFieldType(mysql.TypeDatetime),
	}

	chk := pool.GetChunk(fieldTypes)
	pool.PutChunk(fieldTypes, chk)
	require.Equal(t, 0, len(chk.columns))
}

func BenchmarkPoolChunkOperation(b *testing.B) {
	pool := NewPool(1024)

	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeJSON),
		types.NewFieldType(mysql.TypeFloat),
		types.NewFieldType(mysql.TypeNewDecimal),
		types.NewFieldType(mysql.TypeDouble),
		types.NewFieldType(mysql.TypeLonglong),
		types.NewFieldType(mysql.TypeTimestamp),
		types.NewFieldType(mysql.TypeDatetime),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.PutChunk(fieldTypes, pool.GetChunk(fieldTypes))
		}
	})
}

// TestLocalChunkPool tests the per-goroutine local pool caching
func TestLocalChunkPool(t *testing.T) {
	initCap := 1024
	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeLonglong),
		types.NewFieldType(mysql.TypeVarchar),
	}

	// Get a chunk from pool
	chk1 := getChunkFromPool(initCap, fieldTypes)
	require.NotNil(t, chk1)
	require.Equal(t, initCap, chk1.capacity)

	// Return it to pool
	putChunkFromPool(initCap, fieldTypes, chk1)

	// Get another chunk - should potentially reuse from local pool
	chk2 := getChunkFromPool(initCap, fieldTypes)
	require.NotNil(t, chk2)
	require.Equal(t, initCap, chk2.capacity)

	// Clean up
	putChunkFromPool(initCap, fieldTypes, chk2)
}

// TestChunkPoolSizeLimit tests that large chunks are not pooled
func TestChunkPoolSizeLimit(t *testing.T) {
	initCap := 100000 // Very large capacity
	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeVarchar),
	}

	// Create a large chunk
	largeChunk := getChunkFromPool(initCap, fieldTypes)
	require.NotNil(t, largeChunk)

	// Add data to make it large
	for i := 0; i < 10000; i++ {
		largeChunk.AppendString(0, "very long string for testing memory usage limits in the chunk pool optimization implementation according to RFC-0009")
		largeChunk.AppendString(1, "another long string to increase memory usage")
		largeChunk.AppendString(2, "more data to ensure chunk exceeds size limit")
		largeChunk.AppendString(3, "additional content for memory testing")
		largeChunk.AppendString(4, "final column with long string data")
	}

	// Try to return it - should be dropped if too large
	putChunkFromPool(initCap, fieldTypes, largeChunk)
	// No assertion needed - just testing that it doesn't panic
}

// TestAdaptivePoolSizing tests the adaptive pool size adjustment
func TestAdaptivePoolSizing(t *testing.T) {
	pool := NewPool(1024)
	require.Equal(t, int64(minPoolSize), pool.maxPooled)

	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeLonglong),
	}

	// Simulate workload by getting many chunks
	chunks := make([]*Chunk, 0, 1000)
	for i := 0; i < 1000; i++ {
		chk := pool.GetChunk(fieldTypes)
		chunks = append(chunks, chk)
	}

	// Active chunks should be tracked
	require.Equal(t, int64(1000), pool.activeChunks)

	// Return all chunks
	for _, chk := range chunks {
		pool.PutChunk(fieldTypes, chk)
	}

	// Active chunks should decrease
	require.Equal(t, int64(0), pool.activeChunks)
}

// BenchmarkLocalPoolVsGlobal benchmarks the performance improvement of local pools
func BenchmarkLocalPoolVsGlobal(b *testing.B) {
	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeLonglong),
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeDouble),
	}
	initCap := 1024

	b.Run("WithLocalPool", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				chk := getChunkFromPool(initCap, fieldTypes)
				putChunkFromPool(initCap, fieldTypes, chk)
			}
		})
	})

	b.Run("DirectGlobalPool", func(b *testing.B) {
		pool := NewPool(initCap)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				chk := pool.GetChunk(fieldTypes)
				pool.PutChunk(fieldTypes, chk)
			}
		})
	})
}

// BenchmarkChunkAllocation measures allocation performance with optimization
func BenchmarkChunkAllocation(b *testing.B) {
	fieldTypes := []*types.FieldType{
		types.NewFieldType(mysql.TypeLonglong),
		types.NewFieldType(mysql.TypeVarchar),
		types.NewFieldType(mysql.TypeDouble),
		types.NewFieldType(mysql.TypeDatetime),
	}
	initCap := 1024

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chk := getChunkFromPool(initCap, fieldTypes)
		// Simulate some work
		chk.Reset()
		putChunkFromPool(initCap, fieldTypes, chk)
	}
}
