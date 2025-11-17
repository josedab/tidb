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

package slowlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple select with number",
			input:    "SELECT * FROM products WHERE id = 123",
			expected: "SELECT * FROM products WHERE id = ?",
		},
		{
			name:     "select with string literal",
			input:    "SELECT * FROM users WHERE name = 'John'",
			expected: "SELECT * FROM users WHERE name = ?",
		},
		{
			name:     "multiple values",
			input:    "SELECT * FROM users WHERE name = 'John' AND age = 25",
			expected: "SELECT * FROM users WHERE name = ? AND age = ?",
		},
		{
			name:     "insert statement",
			input:    "INSERT INTO products (id, name, price) VALUES (1, 'Product A', 99.99)",
			expected: "INSERT INTO products (id, name, price) VALUES (?, ?, ?.?)",
		},
		{
			name:     "multiple spaces",
			input:    "SELECT  *  FROM  users   WHERE  id  =  123",
			expected: "SELECT * FROM users WHERE id = ?",
		},
		{
			name:     "numbers in identifiers preserved",
			input:    "SELECT * FROM table123 WHERE col456 = 789",
			expected: "SELECT * FROM table123 WHERE col456 = ?",
		},
		{
			name:     "string with spaces",
			input:    "SELECT * FROM users WHERE name = 'John Doe'",
			expected: "SELECT * FROM users WHERE name = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeQuery(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeQuerySamePattern(t *testing.T) {
	// Different queries should normalize to the same pattern
	queries := []string{
		"SELECT * FROM products WHERE id = 1",
		"SELECT * FROM products WHERE id = 2",
		"SELECT * FROM products WHERE id = 999",
	}

	normalized := make([]string, len(queries))
	for i, q := range queries {
		normalized[i] = NormalizeQuery(q)
	}

	// All should be the same
	for i := 1; i < len(normalized); i++ {
		require.Equal(t, normalized[0], normalized[i], "All queries should normalize to the same pattern")
	}
}

func TestPatternHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		checkLen bool
	}{
		{
			name:     "simple pattern",
			input:    "SELECT * FROM users WHERE id = ?",
			checkLen: true,
		},
		{
			name:     "complex pattern",
			input:    "SELECT * FROM products WHERE category = ? AND price > ? ORDER BY name",
			checkLen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := PatternHash(tt.input)
			if tt.checkLen {
				require.Equal(t, 16, len(hash), "Hash should be 16 characters")
			}
			require.NotEmpty(t, hash, "Hash should not be empty")
		})
	}
}

func TestPatternHashConsistency(t *testing.T) {
	pattern := "SELECT * FROM users WHERE id = ?"

	// Generate hash multiple times
	hash1 := PatternHash(pattern)
	hash2 := PatternHash(pattern)
	hash3 := PatternHash(pattern)

	// All hashes should be identical
	require.Equal(t, hash1, hash2, "Hash should be consistent")
	require.Equal(t, hash2, hash3, "Hash should be consistent")
}

func TestPatternHashUniqueness(t *testing.T) {
	// Different patterns should produce different hashes
	patterns := []string{
		"SELECT * FROM users WHERE id = ?",
		"SELECT * FROM products WHERE id = ?",
		"SELECT * FROM users WHERE name = ?",
	}

	hashes := make(map[string]bool)
	for _, pattern := range patterns {
		hash := PatternHash(pattern)
		require.False(t, hashes[hash], "Different patterns should produce different hashes")
		hashes[hash] = true
	}

	require.Equal(t, len(patterns), len(hashes), "All hashes should be unique")
}
