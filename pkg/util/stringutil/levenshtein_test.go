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

package stringutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLevenshteinDistance(t *testing.T) {
	testCases := []struct {
		s1       string
		s2       string
		expected int
	}{
		// Identical strings
		{"HASH_JOIN", "HASH_JOIN", 0},
		{"", "", 0},

		// One character difference
		{"HASH_JOI", "HASH_JOIN", 1},     // insertion
		{"HASH_JOINN", "HASH_JOIN", 1},   // deletion
		{"HASH_JOON", "HASH_JOIN", 1},    // substitution

		// Multiple character differences
		{"MERGE_JION", "MERGE_JOIN", 1},  // transposition (counted as 1 substitution)
		{"USE_INDX", "USE_INDEX", 1},     // insertion
		{"HASH_AG", "HASH_AGG", 1},       // insertion

		// Completely different strings
		{"ABC", "XYZ", 3},
		{"KITTEN", "SITTING", 3},

		// Empty string cases
		{"", "HASH_JOIN", 9},
		{"HASH_JOIN", "", 9},

		// Case sensitive
		{"hash_join", "HASH_JOIN", 9}, // different case = different chars

		// Practical hint typo examples
		{"INL_JOIN", "HASH_JOIN", 4},
		{"HASHJOIN", "HASH_JOIN", 1},
		{"HASH_JOJN", "HASH_JOIN", 2},
	}

	for _, tc := range testCases {
		t.Run(tc.s1+"_vs_"+tc.s2, func(t *testing.T) {
			result := LevenshteinDistance(tc.s1, tc.s2)
			require.Equal(t, tc.expected, result,
				"LevenshteinDistance(%q, %q) = %d, expected %d",
				tc.s1, tc.s2, result, tc.expected)

			// Distance should be symmetric
			reverseResult := LevenshteinDistance(tc.s2, tc.s1)
			require.Equal(t, tc.expected, reverseResult,
				"LevenshteinDistance should be symmetric")
		})
	}
}

func TestLevenshteinDistance_HintTypos(t *testing.T) {
	// Test realistic hint name typos
	typoTests := []struct {
		typo       string
		correct    string
		maxDist    int // Maximum acceptable distance for suggestion
		shouldMatch bool
	}{
		{"HASH_JOI", "HASH_JOIN", 2, true},
		{"MERGE_JION", "MERGE_JOIN", 2, true},
		{"USE_INDX", "USE_INDEX", 2, true},
		{"HASH_AG", "HASH_AGG", 2, true},
		{"INL_JOI", "INL_JOIN", 2, true},
		{"STREAM_AG", "STREAM_AGG", 2, true},
		{"IGNORE_INDX", "IGNORE_INDEX", 2, true},

		// Should NOT match (too different)
		{"HASH", "MERGE_JOIN", 2, false},
		{"XYZ", "HASH_JOIN", 2, false},
	}

	for _, tc := range typoTests {
		t.Run(tc.typo, func(t *testing.T) {
			dist := LevenshteinDistance(tc.typo, tc.correct)
			if tc.shouldMatch {
				require.LessOrEqual(t, dist, tc.maxDist,
					"Typo %q should match %q (distance %d should be <= %d)",
					tc.typo, tc.correct, dist, tc.maxDist)
			} else {
				require.Greater(t, dist, tc.maxDist,
					"Typo %q should NOT match %q (distance %d should be > %d)",
					tc.typo, tc.correct, dist, tc.maxDist)
			}
		})
	}
}
