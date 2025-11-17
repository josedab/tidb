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

package core_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pingcap/tidb/pkg/testkit"
	"github.com/stretchr/testify/require"
)

func TestHintValidation_UnrecognizedHint(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1, t2")
	tk.MustExec("create table t1(id int, name varchar(50))")
	tk.MustExec("create table t2(id int, val int)")

	// Test unrecognized hint (typo)
	tk.MustQuery("select /*+ HASH_JOI(t1, t2) */ * from t1 join t2 on t1.id = t2.id")

	// Check for warning
	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for unrecognized hint")

	foundTypo := false
	for _, warn := range warnings {
		msg := warn.Err.Error()
		if strings.Contains(msg, "HASH_JOI") &&
			(strings.Contains(msg, "HASH_JOIN") || strings.Contains(msg, "Unrecognized")) {
			foundTypo = true
			break
		}
	}
	require.True(t, foundTypo, "Should have warning about typo with suggestion")
}

func TestHintValidation_UnknownTable(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1, t2")
	tk.MustExec("create table t1(id int, name varchar(50))")
	tk.MustExec("create table t2(id int, val int)")

	// Test hint with unknown table reference
	tk.MustQuery("select /*+ HASH_JOIN(t1, t3) */ * from t1 join t2 on t1.id = t2.id")

	// Check for warning
	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	require.Greater(t, len(warnings), 0, "Should have warning for unknown table")

	foundUnknown := false
	for _, warn := range warnings {
		msg := warn.Err.Error()
		if strings.Contains(msg, "t3") ||
			strings.Contains(msg, "unknown") ||
			strings.Contains(msg, "Available") {
			foundUnknown = true
			break
		}
	}
	require.True(t, foundUnknown, "Should have warning about unknown table")
}

func TestHintValidation_ValidHint(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1, t2")
	tk.MustExec("create table t1(id int, name varchar(50))")
	tk.MustExec("create table t2(id int, val int)")

	// Test valid hint - should not produce validation errors (may have unapplied warnings)
	tk.MustQuery("select /*+ HASH_JOIN(t1, t2) */ * from t1 join t2 on t1.id = t2.id")

	// Check that we don't have unrecognized/unknown table errors
	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()

	for _, warn := range warnings {
		msg := warn.Err.Error()
		// Should not have "Unrecognized" or "unknown table" warnings
		require.NotContains(t, msg, "Unrecognized")
		require.NotContains(t, strings.ToLower(msg), "did you mean", "Valid hint should not suggest alternatives")
	}
}

func TestHintValidation_ConflictingHints(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1, t2")
	tk.MustExec("create table t1(id int, name varchar(50))")
	tk.MustExec("create table t2(id int, val int)")

	// Test conflicting join hints
	tk.MustQuery("select /*+ HASH_JOIN(t1, t2) MERGE_JOIN(t1, t2) */ * from t1 join t2 on t1.id = t2.id")

	// Check for conflict warning
	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()

	foundConflict := false
	for _, warn := range warnings {
		msg := strings.ToLower(warn.Err.Error())
		if strings.Contains(msg, "conflict") {
			foundConflict = true
			break
		}
	}
	require.True(t, foundConflict, "Should have warning about conflicting hints")
}

func TestHintValidation_MissingArguments(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1")
	tk.MustExec("create table t1(id int, name varchar(50))")

	// This test would require modifying the parser to allow hints without required arguments
	// For now, we skip this as the parser already enforces some argument requirements
	t.Skip("Parser enforces some hint argument requirements")
}

func TestHintValidation_MultipleTypos(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1, t2")
	tk.MustExec("create table t1(id int, name varchar(50))")
	tk.MustExec("create table t2(id int, val int)")

	testCases := []struct {
		typo       string
		suggestion string
	}{
		{"HASH_JOI", "HASH_JOIN"},
		{"MERGE_JION", "MERGE_JOIN"},
		{"INL_JOI", "INL_JOIN"},
	}

	for _, tc := range testCases {
		t.Run(tc.typo, func(t *testing.T) {
			query := fmt.Sprintf("select /*+ %s(t1, t2) */ * from t1 join t2 on t1.id = t2.id", tc.typo)
			tk.MustQuery(query)

			warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
			require.Greater(t, len(warnings), 0, "Should have warning for typo %s", tc.typo)

			foundSuggestion := false
			for _, warn := range warnings {
				msg := warn.Err.Error()
				if strings.Contains(msg, tc.suggestion) {
					foundSuggestion = true
					break
				}
			}
			require.True(t, foundSuggestion, "Should suggest %s for typo %s", tc.suggestion, tc.typo)
		})
	}
}

func TestHintValidation_CaseInsensitive(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1, T2")
	tk.MustExec("create table t1(id int)")
	tk.MustExec("create table T2(id int)")

	// Test that table name matching is case-insensitive
	tk.MustQuery("select /*+ HASH_JOIN(T1, t2) */ * from t1 join T2 on t1.id = T2.id")

	// Should not have warnings about unknown tables
	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	for _, warn := range warnings {
		msg := strings.ToLower(warn.Err.Error())
		require.NotContains(t, msg, "unknown table")
		require.NotContains(t, msg, "available tables")
	}
}

func TestHintValidation_TableAlias(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists orders, customers")
	tk.MustExec("create table orders(id int, customer_id int)")
	tk.MustExec("create table customers(id int, name varchar(50))")

	// Test hint using table aliases
	tk.MustQuery("select /*+ HASH_JOIN(o, c) */ * from orders o join customers c on o.customer_id = c.id")

	// Should recognize aliases
	warnings := tk.Session().GetSessionVars().StmtCtx.GetWarnings()
	for _, warn := range warnings {
		msg := warn.Err.Error()
		// Should not complain about unknown table 'o' or 'c'
		require.NotContains(t, msg, "unknown table")
	}
}
