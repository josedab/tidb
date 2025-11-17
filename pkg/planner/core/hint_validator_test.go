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
	"strings"
	"testing"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/testkit"
	"github.com/stretchr/testify/require"
)

func TestHintValidator_UnrecognizedHint(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Test typo detection and suggestion
	testCases := []struct {
		typo       string
		suggestion string
	}{
		{"HASH_JOI", "HASH_JOIN"},
		{"MERGE_JION", "MERGE_JOIN"},
		{"USE_INDX", "USE_INDEX"},
		{"HASH_AG", "HASH_AGG"},
		{"INL_JOI", "INL_JOIN"},
		{"STREAM_AG", "STREAM_AGG"},
	}

	for _, tc := range testCases {
		t.Run(tc.typo, func(t *testing.T) {
			suggestion := validator.findClosestHint(tc.typo)
			require.Equal(t, tc.suggestion, suggestion,
				"findClosestHint(%q) should suggest %q, got %q",
				tc.typo, tc.suggestion, suggestion)
		})
	}
}

func TestHintValidator_NoSuggestionForVeryDifferent(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Completely different strings should not get suggestions
	suggestion := validator.findClosestHint("XYZ")
	require.Equal(t, "", suggestion, "Very different strings should not get suggestions")

	suggestion = validator.findClosestHint("FOOBAR")
	require.Equal(t, "", suggestion, "Very different strings should not get suggestions")
}

func TestHintValidator_MissingTables(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Create hint that requires tables but has none
	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("HASH_JOIN"),
		Tables:   []ast.HintTable{}, // Empty!
	}

	validator.validateSingleHint(hint, []string{})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for missing tables")
	require.Contains(t, warnings[0].Err.Error(), "requires table arguments")
}

func TestHintValidator_MissingIndexes(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Create USE_INDEX hint without index names
	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("USE_INDEX"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t1")},
		},
		Indexes: []ast.CIStr{}, // Empty!
	}

	validator.validateSingleHint(hint, []string{"t1"})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for missing indexes")
	require.Contains(t, warnings[0].Err.Error(), "requires index arguments")
}

func TestHintValidator_UnknownTable(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Create hint referencing non-existent table
	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("HASH_JOIN"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t1")},
			{TableName: ast.NewCIStr("nonexistent")}, // This table doesn't exist
		},
	}

	availableTables := []string{"t1", "t2"}
	validator.validateSingleHint(hint, availableTables)

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for unknown table")
	require.Contains(t, warnings[0].Err.Error(), "nonexistent")
	require.Contains(t, warnings[0].Err.Error(), "Available tables")
}

func TestHintValidator_TooFewTables(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// USE_INDEX requires exactly 1 table but gets 0
	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("USE_INDEX"),
		Tables:   []ast.HintTable{},
		Indexes:  []ast.CIStr{ast.NewCIStr("idx")},
	}

	validator.validateSingleHint(hint, []string{"t1"})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for too few tables")
	require.Contains(t, warnings[0].Err.Error(), "requires")
}

func TestHintValidator_TooManyTables(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// USE_INDEX accepts at most 1 table
	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("USE_INDEX"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t1")},
			{TableName: ast.NewCIStr("t2")}, // Too many!
		},
		Indexes: []ast.CIStr{ast.NewCIStr("idx")},
	}

	validator.validateSingleHint(hint, []string{"t1", "t2"})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for too many tables")
	require.Contains(t, warnings[0].Err.Error(), "at most")
}

func TestHintValidator_ConflictingJoinHints(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Create conflicting join hints
	hints := []*ast.TableOptimizerHint{
		{
			HintName: ast.NewCIStr("HASH_JOIN"),
			Tables: []ast.HintTable{
				{TableName: ast.NewCIStr("t1")},
				{TableName: ast.NewCIStr("t2")},
			},
		},
		{
			HintName: ast.NewCIStr("MERGE_JOIN"),
			Tables: []ast.HintTable{
				{TableName: ast.NewCIStr("t1")},
				{TableName: ast.NewCIStr("t2")},
			},
		},
	}

	validator.ValidateHints(hints, []string{"t1", "t2"})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for conflicting hints")

	// Check for conflict warning
	foundConflict := false
	for _, warn := range warnings {
		if containsString(warn.Err.Error(), "Conflicting") ||
			containsString(warn.Err.Error(), "conflicting") {
			foundConflict = true
			break
		}
	}
	require.True(t, foundConflict, "Should have warning about conflicting hints")
}

func TestHintValidator_ConflictingAggHints(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Create conflicting agg hints
	hints := []*ast.TableOptimizerHint{
		{
			HintName: ast.NewCIStr("HASH_AGG"),
		},
		{
			HintName: ast.NewCIStr("STREAM_AGG"),
		},
	}

	validator.ValidateHints(hints, []string{"t1"})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for conflicting hints")
}

func TestHintValidator_ValidHint(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Create valid hint
	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("HASH_JOIN"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t1")},
			{TableName: ast.NewCIStr("t2")},
		},
	}

	validator.validateSingleHint(hint, []string{"t1", "t2"})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	// Valid hint should not generate warnings during validation
	// (Note: warnings about unapplied hints come later via WarnUnappliedHints)
	require.Equal(t, 0, len(warnings), "Valid hint should not generate validation warnings")
}

func TestHintValidator_MarkHintApplied(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	hint := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("HASH_JOIN"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t1")},
		},
	}

	// Initially not applied
	require.False(t, validator.appliedHints[hint])

	// Mark as applied
	validator.MarkHintApplied(hint)

	// Now should be marked
	require.True(t, validator.appliedHints[hint])
}

func TestHintValidator_WarnUnappliedHints(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	hint1 := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("HASH_JOIN"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t1")},
		},
	}

	hint2 := &ast.TableOptimizerHint{
		HintName: ast.NewCIStr("MERGE_JOIN"),
		Tables: []ast.HintTable{
			{TableName: ast.NewCIStr("t2")},
		},
	}

	// Mark only hint1 as applied
	validator.MarkHintApplied(hint1)

	// Warn about unapplied hints
	validator.WarnUnappliedHints([]*ast.TableOptimizerHint{hint1, hint2})

	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	// Should have warning for hint2 (not applied)
	require.Greater(t, len(warnings), 0, "Should have warning for unapplied hint")
	require.Contains(t, warnings[0].Err.Error(), "MERGE_JOIN")
}

func TestHintValidator_TableExists(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	available := []string{"table1", "Table2", "TABLE3"}

	// Case-insensitive matching
	require.True(t, validator.tableExists("table1", available))
	require.True(t, validator.tableExists("TABLE1", available))
	require.True(t, validator.tableExists("Table2", available))
	require.True(t, validator.tableExists("table3", available))

	// Non-existent table
	require.False(t, validator.tableExists("nonexistent", available))
}

func TestHintValidator_IsJoinHint(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Join hints
	require.True(t, validator.isJoinHint("HASH_JOIN"))
	require.True(t, validator.isJoinHint("MERGE_JOIN"))
	require.True(t, validator.isJoinHint("INL_JOIN"))
	require.True(t, validator.isJoinHint("INL_HASH_JOIN"))

	// Case insensitive
	require.True(t, validator.isJoinHint("hash_join"))

	// Non-join hints
	require.False(t, validator.isJoinHint("HASH_AGG"))
	require.False(t, validator.isJoinHint("USE_INDEX"))
	require.False(t, validator.isJoinHint("MEMORY_QUOTA"))
}

func TestHintValidator_IsAggHint(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	validator := NewHintValidator(tk.Session())

	// Agg hints
	require.True(t, validator.isAggHint("HASH_AGG"))
	require.True(t, validator.isAggHint("STREAM_AGG"))
	require.True(t, validator.isAggHint("MPP_1PHASE_AGG"))
	require.True(t, validator.isAggHint("MPP_2PHASE_AGG"))

	// Case insensitive
	require.True(t, validator.isAggHint("hash_agg"))

	// Non-agg hints
	require.False(t, validator.isAggHint("HASH_JOIN"))
	require.False(t, validator.isAggHint("USE_INDEX"))
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		strings.Contains(s, substr)))
}
