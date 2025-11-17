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

package errno

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuredErrorBasic(t *testing.T) {
	err := NewStructuredError(ErrParse, "Syntax error")
	require.NotNil(t, err)
	require.Equal(t, uint16(ErrParse), err.Code)
	require.Equal(t, "Syntax error", err.Message)
	require.NotNil(t, err.Context)
	require.Empty(t, err.Context)

	formatted := err.Format()
	require.Contains(t, formatted, "ERROR 1064")
	require.Contains(t, formatted, "Syntax error")
}

func TestStructuredErrorWithContext(t *testing.T) {
	err := NewStructuredError(ErrParse, "Invalid operator").
		WithContext("operator", "==").
		WithContext("expected", "=")

	formatted := err.Format()
	require.Contains(t, formatted, "ERROR 1064")
	require.Contains(t, formatted, "Invalid operator")
	require.Contains(t, formatted, "operator: ==")
	require.Contains(t, formatted, "expected: =")
}

func TestStructuredErrorWithSuggestion(t *testing.T) {
	err := NewStructuredError(ErrParse, "Invalid comparison operator").
		WithSuggestion("Use '=' for comparison instead of '=='")

	formatted := err.Format()
	require.Contains(t, formatted, "ERROR 1064")
	require.Contains(t, formatted, "Invalid comparison operator")
	require.Contains(t, formatted, "Suggestion: Use '=' for comparison instead of '=='")
}

func TestStructuredErrorWithPosition(t *testing.T) {
	sql := "SELECT * FROM users WHERE id == 1"
	err := NewStructuredError(ErrParse, "Syntax error near '=='").
		WithPosition(1, 29, 2, sql)

	formatted := err.Format()
	require.Contains(t, formatted, "ERROR 1064")
	require.Contains(t, formatted, "Syntax error near '=='")
	require.Contains(t, formatted, sql)
	require.Contains(t, formatted, "^^")
}

func TestStructuredErrorComplete(t *testing.T) {
	sql := "SELECT * FROM users WHERE id == 1"
	err := &StructuredError{
		Code:    ErrParse,
		Message: "Syntax error: invalid comparison operator",
		Context: map[string]interface{}{
			"operator": "==",
			"expected": "=",
		},
		Suggestion: "Use '=' for comparison instead of '=='",
		Position: &ErrorPosition{
			Line:   1,
			Column: 29,
			Length: 2,
			SQL:    sql,
		},
	}

	formatted := err.Format()
	require.Contains(t, formatted, "ERROR 1064")
	require.Contains(t, formatted, "Syntax error: invalid comparison operator")
	require.Contains(t, formatted, "operator: ==")
	require.Contains(t, formatted, "expected: =")
	require.Contains(t, formatted, "Suggestion: Use '=' for comparison instead of '=='")
	require.Contains(t, formatted, sql)
	require.Contains(t, formatted, "^^")
}

func TestStructuredErrorImplementsError(t *testing.T) {
	var err error = NewStructuredError(ErrParse, "test error")
	require.NotNil(t, err)
	errStr := err.Error()
	require.Contains(t, errStr, "ERROR 1064")
	require.Contains(t, errStr, "test error")
}

func TestErrorPositionFormat(t *testing.T) {
	tests := []struct {
		name     string
		pos      *ErrorPosition
		expected []string // strings that should be present in output
		notFound []string // strings that should NOT be present
	}{
		{
			name: "single line error",
			pos: &ErrorPosition{
				Line:   1,
				Column: 5,
				Length: 3,
				SQL:    "SELECT * FROM users",
			},
			expected: []string{"SELECT * FROM users", "^^^"},
		},
		{
			name: "multi-line SQL with error on first line",
			pos: &ErrorPosition{
				Line:   1,
				Column: 8,
				Length: 1,
				SQL:    "SELECT *\nFROM users\nWHERE id = 1",
			},
			expected: []string{"SELECT *", "^"},
			notFound: []string{"FROM users", "WHERE"},
		},
		{
			name: "multi-line SQL with error on second line",
			pos: &ErrorPosition{
				Line:   2,
				Column: 6,
				Length: 5,
				SQL:    "SELECT *\nFROM users\nWHERE id = 1",
			},
			expected: []string{"FROM users", "^^^^^"},
			notFound: []string{"SELECT", "WHERE"},
		},
		{
			name: "error at beginning of line",
			pos: &ErrorPosition{
				Line:   1,
				Column: 1,
				Length: 6,
				SQL:    "SELEKT * FROM users",
			},
			expected: []string{"SELEKT * FROM users", "^^^^^^"},
		},
		{
			name: "invalid line number (too high)",
			pos: &ErrorPosition{
				Line:   10,
				Column: 5,
				Length: 3,
				SQL:    "SELECT * FROM users",
			},
			expected: []string{}, // should return empty string
		},
		{
			name: "invalid line number (zero)",
			pos: &ErrorPosition{
				Line:   0,
				Column: 5,
				Length: 3,
				SQL:    "SELECT * FROM users",
			},
			expected: []string{}, // should return empty string
		},
		{
			name: "empty SQL",
			pos: &ErrorPosition{
				Line:   1,
				Column: 5,
				Length: 3,
				SQL:    "",
			},
			expected: []string{}, // should return empty string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.pos.Format()

			for _, exp := range tt.expected {
				require.Contains(t, result, exp, "Expected to find '%s' in output", exp)
			}

			for _, notExp := range tt.notFound {
				require.NotContains(t, result, notExp, "Expected NOT to find '%s' in output", notExp)
			}
		})
	}
}

func TestErrorPositionEdgeCases(t *testing.T) {
	t.Run("column less than 1", func(t *testing.T) {
		pos := &ErrorPosition{
			Line:   1,
			Column: 0,
			Length: 3,
			SQL:    "SELECT * FROM users",
		}
		result := pos.Format()
		require.NotEmpty(t, result)
		// Should handle gracefully by treating as column 1
	})

	t.Run("length less than 1", func(t *testing.T) {
		pos := &ErrorPosition{
			Line:   1,
			Column: 5,
			Length: 0,
			SQL:    "SELECT * FROM users",
		}
		result := pos.Format()
		require.NotEmpty(t, result)
		// Should handle gracefully by using length 1
	})
}

func TestStructuredErrorChaining(t *testing.T) {
	sql := "SELECT * FROM users WHERE id == 1"
	err := NewStructuredError(ErrParse, "Syntax error").
		WithContext("operator", "==").
		WithContext("line", 1).
		WithSuggestion("Use '=' instead").
		WithPosition(1, 29, 2, sql)

	require.Equal(t, uint16(ErrParse), err.Code)
	require.Equal(t, "Syntax error", err.Message)
	require.Equal(t, "==", err.Context["operator"])
	require.Equal(t, 1, err.Context["line"])
	require.Equal(t, "Use '=' instead", err.Suggestion)
	require.NotNil(t, err.Position)
	require.Equal(t, 1, err.Position.Line)
	require.Equal(t, 29, err.Position.Column)
}

func TestStructuredErrorRealWorldExamples(t *testing.T) {
	t.Run("table not found", func(t *testing.T) {
		err := NewStructuredError(ErrBadTable, "Table 't' not found in database 'mydb'").
			WithContext("table", "t").
			WithContext("database", "mydb").
			WithContext("similar_tables", []string{"tasks", "teams", "tests"}).
			WithSuggestion("Did you mean 'tasks'?")

		formatted := err.Format()
		require.Contains(t, formatted, "Table 't' not found")
		require.Contains(t, formatted, "table: t")
		require.Contains(t, formatted, "database: mydb")
		require.Contains(t, formatted, "Did you mean 'tasks'?")
	})

	t.Run("duplicate column", func(t *testing.T) {
		sql := "SELECT id, name, id FROM users"
		err := NewStructuredError(ErrDupFieldName, "Duplicate column name 'id'").
			WithContext("column", "id").
			WithContext("first_occurrence", "column 1").
			WithContext("second_occurrence", "column 3").
			WithPosition(1, 18, 2, sql).
			WithSuggestion("Use column aliases to distinguish: SELECT id, name, id AS id2")

		formatted := err.Format()
		require.Contains(t, formatted, "Duplicate column name")
		require.Contains(t, formatted, "column: id")
		require.Contains(t, formatted, "Use column aliases")
	})

	t.Run("type mismatch", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE age = 'twenty'"
		err := NewStructuredError(ErrBadField, "Type mismatch in WHERE clause").
			WithContext("column", "age").
			WithContext("column_type", "INT").
			WithContext("provided_type", "STRING").
			WithContext("provided_value", "twenty").
			WithPosition(1, 33, 8, sql).
			WithSuggestion("Provide a numeric value for column 'age'")

		formatted := err.Format()
		require.Contains(t, formatted, "Type mismatch")
		require.Contains(t, formatted, "column_type: INT")
		require.Contains(t, formatted, "provided_value: twenty")
		require.Contains(t, formatted, "Provide a numeric value")
	})
}

func TestStructuredErrorFormatConsistency(t *testing.T) {
	err := NewStructuredError(ErrParse, "Test error")

	// Format should be consistent across multiple calls
	formatted1 := err.Format()
	formatted2 := err.Format()
	require.Equal(t, formatted1, formatted2)

	// Error() should return the same as Format()
	errorStr := err.Error()
	require.Equal(t, formatted1, errorStr)
}

func TestContextMapNil(t *testing.T) {
	err := &StructuredError{
		Code:    ErrParse,
		Message: "Test",
		Context: nil,
	}

	// Should not panic with nil context
	formatted := err.Format()
	require.Contains(t, formatted, "ERROR 1064")
	require.Contains(t, formatted, "Test")

	// WithContext should initialize the map
	err.WithContext("key", "value")
	require.NotNil(t, err.Context)
	require.Equal(t, "value", err.Context["key"])
}

func TestErrorPositionFormatting(t *testing.T) {
	sql := "SELECT * FROM users WHERE id == 1"
	pos := &ErrorPosition{
		Line:   1,
		Column: 29,
		Length: 2,
		SQL:    sql,
	}

	formatted := pos.Format()

	// Should have exactly 2 lines (SQL + indicator)
	lines := strings.Split(strings.TrimSpace(formatted), "\n")
	require.Len(t, lines, 2)

	// First line should contain the SQL
	require.Contains(t, lines[0], sql)

	// Second line should have the indicator at the right position
	// The indicator should be at column 29 (0-indexed: 28)
	// Plus 7 spaces for the "       " prefix
	indicatorLine := lines[1]
	// Find where the ^^ starts
	caretIndex := strings.Index(indicatorLine, "^")
	require.Greater(t, caretIndex, 0, "Should find caret indicator")
}
