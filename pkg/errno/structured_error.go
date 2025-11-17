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
	"fmt"
	"strings"
)

// StructuredError provides rich error information with context,
// suggestions, and precise error location for better debugging experience.
//
// Example usage:
//
//	err := &StructuredError{
//	    Code:    ErrParse,
//	    Message: "Syntax error: invalid comparison operator",
//	    Context: map[string]interface{}{
//	        "operator": "==",
//	        "expected": "=",
//	    },
//	    Suggestion: "Use '=' for comparison instead of '=='",
//	    Position: &ErrorPosition{
//	        Line:   1,
//	        Column: 28,
//	        Length: 2,
//	        SQL:    "SELECT * FROM users WHERE id == 1",
//	    },
//	}
type StructuredError struct {
	Code       uint16                 // MySQL error code
	Message    string                 // Human-readable error message
	Context    map[string]interface{} // Diagnostic data (table names, values, etc.)
	Suggestion string                 // Actionable fix suggestion
	Position   *ErrorPosition         // Location in SQL where error occurred
}

// ErrorPosition pinpoints the exact location of an error in SQL text
// and provides visual indication of the problematic section.
type ErrorPosition struct {
	Line   int    // Line number (1-indexed)
	Column int    // Column number (1-indexed)
	Length int    // Length of the error token
	SQL    string // The complete SQL statement
}

// Error implements the error interface, returning the formatted error message.
func (e *StructuredError) Error() string {
	return e.Format()
}

// Format returns a user-friendly error message with all available context.
// The output includes:
//   - Error code and main message
//   - Context information (if available)
//   - Actionable suggestion (if available)
//   - Visual position indicator (if available)
func (e *StructuredError) Format() string {
	var sb strings.Builder

	// Main error message
	sb.WriteString(fmt.Sprintf("ERROR %d: %s", e.Code, e.Message))

	// Add context information
	if len(e.Context) > 0 {
		sb.WriteString("\n")
		for key, val := range e.Context {
			sb.WriteString(fmt.Sprintf("  %s: %v\n", key, val))
		}
	}

	// Add suggestion
	if e.Suggestion != "" {
		sb.WriteString(fmt.Sprintf("Suggestion: %s\n", e.Suggestion))
	}

	// Add position indicator
	if e.Position != nil {
		posStr := e.Position.Format()
		if posStr != "" {
			sb.WriteString(posStr)
		}
	}

	return sb.String()
}

// Format formats the error position with visual indicators.
// It shows the relevant line of SQL with a caret (^) pointing
// to the exact location of the error.
//
// Example output:
//
//	       SELECT * FROM users WHERE id == 1;
//	                                    ^^
func (ep *ErrorPosition) Format() string {
	if ep.SQL == "" {
		return ""
	}

	lines := strings.Split(ep.SQL, "\n")
	if ep.Line <= 0 || ep.Line > len(lines) {
		return ""
	}

	line := lines[ep.Line-1]

	// Create visual indicator
	// Ensure column is valid
	column := ep.Column
	if column < 1 {
		column = 1
	}

	length := ep.Length
	if length < 1 {
		length = 1
	}

	// Build indicator with proper spacing
	spaces := strings.Repeat(" ", column-1)
	carets := strings.Repeat("^", length)
	indicator := spaces + carets

	return fmt.Sprintf("       %s\n       %s\n", line, indicator)
}

// NewStructuredError creates a new StructuredError with the given error code and message.
func NewStructuredError(code uint16, message string) *StructuredError {
	return &StructuredError{
		Code:    code,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// WithContext adds context information to the error.
func (e *StructuredError) WithContext(key string, value interface{}) *StructuredError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithSuggestion adds an actionable suggestion to the error.
func (e *StructuredError) WithSuggestion(suggestion string) *StructuredError {
	e.Suggestion = suggestion
	return e
}

// WithPosition adds position information to the error.
func (e *StructuredError) WithPosition(line, column, length int, sql string) *StructuredError {
	e.Position = &ErrorPosition{
		Line:   line,
		Column: column,
		Length: length,
		SQL:    sql,
	}
	return e
}
