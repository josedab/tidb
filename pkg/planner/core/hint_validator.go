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
	"slices"
	"strings"

	"github.com/pingcap/tidb/pkg/errno"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/sessionctx"
	"github.com/pingcap/tidb/pkg/sessionctx/stmtctx"
	"github.com/pingcap/tidb/pkg/util/dbterror"
	"github.com/pingcap/tidb/pkg/util/stringutil"
)

// HintValidator validates query hints and generates warnings for invalid/ignored hints
type HintValidator struct {
	ctx          sessionctx.Context
	validHints   map[string]HintMetadata
	appliedHints map[*ast.TableOptimizerHint]bool
}

// HintMetadata describes requirements and constraints for a hint
type HintMetadata struct {
	Name            string
	RequiresTables  bool     // Does hint need table arguments?
	RequiresIndexes bool     // Does hint need index arguments?
	MinTables       int      // Minimum table count
	MaxTables       int      // Maximum table count (0 = unlimited)
	ApplicableOps   []string // Which operators can use this hint? (join, agg, scan, etc.)
}

// Error definitions for hint validation
var (
	ErrWarnOptimizerHintInvalidArguments = dbterror.ClassOptimizer.NewStd(errno.ErrWarnOptimizerHintInvalidArguments)
	ErrWarnOptimizerHintUnknownTable     = dbterror.ClassOptimizer.NewStd(errno.ErrWarnOptimizerHintUnknownTable)
	ErrWarnOptimizerHintNotApplied       = dbterror.ClassOptimizer.NewStd(errno.ErrWarnOptimizerHintNotApplied)
	ErrUnresolvedHintName                = dbterror.ClassOptimizer.NewStd(errno.ErrUnresolvedHintName)
	ErrWarnConflictingHint               = dbterror.ClassOptimizer.NewStd(errno.ErrWarnConflictingHint)
)

// NewHintValidator creates a new hint validator
func NewHintValidator(ctx sessionctx.Context) *HintValidator {
	return &HintValidator{
		ctx:          ctx,
		validHints:   buildHintMetadataMap(),
		appliedHints: make(map[*ast.TableOptimizerHint]bool),
	}
}

// buildHintMetadataMap returns metadata for all valid hints in TiDB
func buildHintMetadataMap() map[string]HintMetadata {
	return map[string]HintMetadata{
		// Join algorithm hints
		"HASH_JOIN": {
			Name:           "HASH_JOIN",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"MERGE_JOIN": {
			Name:           "MERGE_JOIN",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"INL_JOIN": {
			Name:           "INL_JOIN",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"INL_HASH_JOIN": {
			Name:           "INL_HASH_JOIN",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"INL_MERGE_JOIN": {
			Name:           "INL_MERGE_JOIN",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"HASH_JOIN_BUILD": {
			Name:           "HASH_JOIN_BUILD",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"HASH_JOIN_PROBE": {
			Name:           "HASH_JOIN_PROBE",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},
		"NO_HASH_JOIN": {
			Name:           "NO_HASH_JOIN",
			RequiresTables: false,
			ApplicableOps:  []string{"join"},
		},
		"NO_MERGE_JOIN": {
			Name:           "NO_MERGE_JOIN",
			RequiresTables: false,
			ApplicableOps:  []string{"join"},
		},

		// Aggregation hints
		"HASH_AGG": {
			Name:          "HASH_AGG",
			RequiresTables: false,
			ApplicableOps: []string{"agg"},
		},
		"STREAM_AGG": {
			Name:          "STREAM_AGG",
			RequiresTables: false,
			ApplicableOps: []string{"agg"},
		},
		"MPP_1PHASE_AGG": {
			Name:          "MPP_1PHASE_AGG",
			RequiresTables: false,
			ApplicableOps: []string{"agg"},
		},
		"MPP_2PHASE_AGG": {
			Name:          "MPP_2PHASE_AGG",
			RequiresTables: false,
			ApplicableOps: []string{"agg"},
		},

		// Index hints
		"USE_INDEX": {
			Name:            "USE_INDEX",
			RequiresTables:  true,
			RequiresIndexes: true,
			MinTables:       1,
			MaxTables:       1,
			ApplicableOps:   []string{"scan"},
		},
		"IGNORE_INDEX": {
			Name:            "IGNORE_INDEX",
			RequiresTables:  true,
			RequiresIndexes: true,
			MinTables:       1,
			MaxTables:       1,
			ApplicableOps:   []string{"scan"},
		},
		"FORCE_INDEX": {
			Name:            "FORCE_INDEX",
			RequiresTables:  true,
			RequiresIndexes: true,
			MinTables:       1,
			MaxTables:       1,
			ApplicableOps:   []string{"scan"},
		},
		"ORDER_INDEX": {
			Name:            "ORDER_INDEX",
			RequiresTables:  true,
			RequiresIndexes: true,
			MinTables:       1,
			MaxTables:       1,
			ApplicableOps:   []string{"scan"},
		},
		"NO_ORDER_INDEX": {
			Name:            "NO_ORDER_INDEX",
			RequiresTables:  true,
			RequiresIndexes: true,
			MinTables:       1,
			MaxTables:       1,
			ApplicableOps:   []string{"scan"},
		},
		"INDEX_MERGE": {
			Name:           "INDEX_MERGE",
			RequiresTables: true,
			MinTables:      1,
			MaxTables:      1,
			ApplicableOps:  []string{"scan"},
		},
		"NO_INDEX_MERGE": {
			Name:           "NO_INDEX_MERGE",
			RequiresTables: false,
			ApplicableOps:  []string{"scan"},
		},

		// Join order hints
		"LEADING": {
			Name:           "LEADING",
			RequiresTables: true,
			MinTables:      1,
			ApplicableOps:  []string{"join"},
		},

		// Other hints
		"MEMORY_QUOTA": {
			Name:          "MEMORY_QUOTA",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"MAX_EXECUTION_TIME": {
			Name:          "MAX_EXECUTION_TIME",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"READ_FROM_STORAGE": {
			Name:           "READ_FROM_STORAGE",
			RequiresTables: false,
			ApplicableOps:  []string{"any"},
		},
		"USE_TOJA": {
			Name:          "USE_TOJA",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"NO_DECORRELATE": {
			Name:          "NO_DECORRELATE",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"LIMIT_TO_COP": {
			Name:          "LIMIT_TO_COP",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"AGG_TO_COP": {
			Name:          "AGG_TO_COP",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"IGNORE_PLAN_CACHE": {
			Name:          "IGNORE_PLAN_CACHE",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
		"STRAIGHT_JOIN": {
			Name:          "STRAIGHT_JOIN",
			RequiresTables: false,
			ApplicableOps: []string{"join"},
		},
		"QB_NAME": {
			Name:          "QB_NAME",
			RequiresTables: false,
			ApplicableOps: []string{"any"},
		},
	}
}

// ValidateHints validates all hints in a query
func (v *HintValidator) ValidateHints(hints []*ast.TableOptimizerHint, availableTables []string) {
	if len(hints) == 0 {
		return
	}

	for _, hint := range hints {
		v.validateSingleHint(hint, availableTables)
	}

	// Check for conflicts
	v.detectConflicts(hints)
}

// validateSingleHint validates one hint
func (v *HintValidator) validateSingleHint(hint *ast.TableOptimizerHint, availableTables []string) {
	if hint == nil {
		return
	}

	hintName := hint.HintName.String()

	// Check 1: Is hint name recognized?
	metadata, exists := v.validHints[strings.ToUpper(hintName)]
	if !exists {
		v.warnUnrecognizedHint(hintName)
		return
	}

	// Check 2: Does hint have required arguments?
	if metadata.RequiresTables && len(hint.Tables) == 0 {
		v.warnMissingTables(hintName)
		return
	}

	if metadata.RequiresIndexes && len(hint.Indexes) == 0 {
		v.warnMissingIndexes(hintName)
		return
	}

	// Check 3: Are table counts correct?
	tableCount := len(hint.Tables)
	if metadata.MinTables > 0 && tableCount < metadata.MinTables {
		v.warnTooFewTables(hintName, tableCount, metadata.MinTables)
		return
	}

	if metadata.MaxTables > 0 && tableCount > metadata.MaxTables {
		v.warnTooManyTables(hintName, tableCount, metadata.MaxTables)
		return
	}

	// Check 4: Do referenced tables exist?
	if metadata.RequiresTables && len(availableTables) > 0 {
		for _, hintTable := range hint.Tables {
			tableName := hintTable.TableName.String()
			if tableName != "" && !v.tableExists(tableName, availableTables) {
				v.warnUnknownTable(hintName, tableName, availableTables)
			}
		}
	}
}

// detectConflicts finds conflicting hints
func (v *HintValidator) detectConflicts(hints []*ast.TableOptimizerHint) {
	// Group hints by target tables
	hintsByTables := make(map[string][]*ast.TableOptimizerHint)

	for _, hint := range hints {
		// Create key from sorted table names
		key := v.tableKey(hint.Tables)
		hintsByTables[key] = append(hintsByTables[key], hint)
	}

	// Check for conflicts within each group
	for _, groupHints := range hintsByTables {
		if len(groupHints) > 1 {
			v.checkHintGroupConflicts(groupHints)
		}
	}
}

// checkHintGroupConflicts checks if hints in a group conflict
func (v *HintValidator) checkHintGroupConflicts(hints []*ast.TableOptimizerHint) {
	// Join algorithm conflicts
	joinHints := []string{}
	for _, hint := range hints {
		name := hint.HintName.String()
		if v.isJoinHint(name) {
			joinHints = append(joinHints, name)
		}
	}

	if len(joinHints) > 1 {
		v.warnConflictingHints(joinHints, "multiple join algorithms specified for the same tables")
	}

	// Aggregation algorithm conflicts
	aggHints := []string{}
	for _, hint := range hints {
		name := hint.HintName.String()
		if v.isAggHint(name) {
			aggHints = append(aggHints, name)
		}
	}

	if len(aggHints) > 1 {
		v.warnConflictingHints(aggHints, "multiple aggregation algorithms specified")
	}
}

// Warning generation methods

func (v *HintValidator) warnUnrecognizedHint(hintName string) {
	// Find closest match using Levenshtein distance
	suggestion := v.findClosestHint(hintName)

	var msg string
	if suggestion != "" {
		msg = fmt.Sprintf("Unrecognized hint '%s'. Did you mean '%s'?", hintName, suggestion)
	} else {
		msg = fmt.Sprintf("Unrecognized hint '%s'", hintName)
	}

	v.appendWarning(ErrUnresolvedHintName.FastGenByArgs(msg))
}

func (v *HintValidator) warnMissingTables(hintName string) {
	msg := fmt.Sprintf("requires table arguments")
	v.appendWarning(ErrWarnOptimizerHintInvalidArguments.FastGenByArgs(hintName, msg))
}

func (v *HintValidator) warnMissingIndexes(hintName string) {
	msg := fmt.Sprintf("requires index arguments")
	v.appendWarning(ErrWarnOptimizerHintInvalidArguments.FastGenByArgs(hintName, msg))
}

func (v *HintValidator) warnTooFewTables(hintName string, got, need int) {
	msg := fmt.Sprintf("requires at least %d tables, got %d", need, got)
	v.appendWarning(ErrWarnOptimizerHintInvalidArguments.FastGenByArgs(hintName, msg))
}

func (v *HintValidator) warnTooManyTables(hintName string, got, max int) {
	msg := fmt.Sprintf("accepts at most %d tables, got %d", max, got)
	v.appendWarning(ErrWarnOptimizerHintInvalidArguments.FastGenByArgs(hintName, msg))
}

func (v *HintValidator) warnUnknownTable(hintName, tableName string, available []string) {
	msg := fmt.Sprintf("references unknown table '%s'. Available tables: %s",
		tableName, strings.Join(available, ", "))
	v.appendWarning(ErrWarnOptimizerHintInvalidArguments.FastGenByArgs(hintName, msg))
}

func (v *HintValidator) warnConflictingHints(hints []string, reason string) {
	msg := fmt.Sprintf("Conflicting hints: %s (%s). Using '%s'",
		strings.Join(hints, ", "), reason, hints[0])
	v.appendWarning(ErrWarnConflictingHint.FastGenByArgs(msg))
}

// Helper methods

// findClosestHint finds the closest valid hint name using fuzzy matching
func (v *HintValidator) findClosestHint(input string) string {
	input = strings.ToUpper(input)
	minDistance := 999
	closest := ""

	for validHint := range v.validHints {
		distance := stringutil.LevenshteinDistance(input, validHint)
		// Only suggest if distance is small enough (max 2 char difference)
		if distance < minDistance && distance <= 2 {
			minDistance = distance
			closest = validHint
		}
	}

	return closest
}

// tableExists checks if a table name exists in the available tables
func (v *HintValidator) tableExists(tableName string, available []string) bool {
	tableName = strings.ToLower(tableName)
	for _, avail := range available {
		if strings.ToLower(avail) == tableName {
			return true
		}
	}
	return false
}

// tableKey creates a unique key from table names for grouping
func (v *HintValidator) tableKey(tables []ast.HintTable) string {
	if len(tables) == 0 {
		return ""
	}

	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = t.TableName.L
	}
	// Sort for consistent key
	slices.Sort(names)
	return strings.Join(names, ",")
}

// isJoinHint checks if a hint is a join algorithm hint
func (v *HintValidator) isJoinHint(hintName string) bool {
	joinHints := []string{
		"HASH_JOIN", "MERGE_JOIN", "INL_JOIN",
		"INL_HASH_JOIN", "INL_MERGE_JOIN",
		"HASH_JOIN_BUILD", "HASH_JOIN_PROBE",
	}
	hintNameUpper := strings.ToUpper(hintName)
	for _, jh := range joinHints {
		if hintNameUpper == jh {
			return true
		}
	}
	return false
}

// isAggHint checks if a hint is an aggregation algorithm hint
func (v *HintValidator) isAggHint(hintName string) bool {
	aggHints := []string{"HASH_AGG", "STREAM_AGG", "MPP_1PHASE_AGG", "MPP_2PHASE_AGG"}
	hintNameUpper := strings.ToUpper(hintName)
	for _, ah := range aggHints {
		if hintNameUpper == ah {
			return true
		}
	}
	return false
}

// MarkHintApplied marks a hint as successfully applied
func (v *HintValidator) MarkHintApplied(hint *ast.TableOptimizerHint) {
	if hint == nil {
		return
	}
	v.appliedHints[hint] = true
}

// WarnUnappliedHints generates warnings for hints that weren't applied
func (v *HintValidator) WarnUnappliedHints(allHints []*ast.TableOptimizerHint) {
	for _, hint := range allHints {
		if hint == nil {
			continue
		}
		// Only warn if hint was recognized but not applied
		hintName := hint.HintName.String()
		_, isValid := v.validHints[strings.ToUpper(hintName)]
		if isValid && !v.appliedHints[hint] {
			msg := "query structure did not match hint requirements"
			v.appendWarning(ErrWarnOptimizerHintNotApplied.FastGenByArgs(hintName, msg))
		}
	}
}

// appendWarning appends a warning to the statement context
func (v *HintValidator) appendWarning(err error) {
	if v.ctx == nil {
		return
	}
	sessVars := v.ctx.GetSessionVars()
	if sessVars == nil {
		return
	}
	stmtCtx := sessVars.StmtCtx
	if stmtCtx == nil {
		return
	}
	stmtCtx.AppendWarning(err)
}

// GetAppliedHints returns the set of hints that were successfully applied
func (v *HintValidator) GetAppliedHints() map[*ast.TableOptimizerHint]bool {
	return v.appliedHints
}
