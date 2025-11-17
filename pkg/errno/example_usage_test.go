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

package errno_test

import (
	"fmt"

	"github.com/pingcap/tidb/pkg/errno"
)

// ExampleStructuredError_basic demonstrates basic usage of StructuredError
func ExampleStructuredError_basic() {
	err := errno.NewStructuredError(
		errno.ErrParse,
		"Syntax error in SQL statement",
	)

	fmt.Println(err.Error())
	// Output:
	// ERROR 1064: Syntax error in SQL statement
}

// ExampleStructuredError_withContext shows how to add contextual information
func ExampleStructuredError_withContext() {
	err := errno.NewStructuredError(
		errno.ErrBadTable,
		"Table not found",
	).
		WithContext("table", "user").
		WithContext("database", "mydb")

	// Note: Output order of context fields may vary due to map iteration
	fmt.Println(err.Code)
	fmt.Println(err.Context["table"])
	fmt.Println(err.Context["database"])
	// Output:
	// 1051
	// user
	// mydb
}

// ExampleStructuredError_withSuggestion demonstrates adding actionable suggestions
func ExampleStructuredError_withSuggestion() {
	err := errno.NewStructuredError(
		errno.ErrParse,
		"Invalid comparison operator",
	).
		WithSuggestion("Use '=' for comparison instead of '=='")

	fmt.Println(err.Suggestion)
	// Output:
	// Use '=' for comparison instead of '=='
}

// ExampleStructuredError_complete shows a fully-featured error with all components
func ExampleStructuredError_complete() {
	sql := "SELECT * FROM users WHERE id == 1"

	err := errno.NewStructuredError(
		errno.ErrParse,
		"Syntax error: invalid comparison operator",
	).
		WithContext("operator", "==").
		WithContext("expected", "=").
		WithSuggestion("Use '=' for comparison instead of '=='").
		WithPosition(1, 29, 2, sql)

	// Demonstrate that all components are set
	fmt.Printf("Code: %d\n", err.Code)
	fmt.Printf("Message: %s\n", err.Message)
	fmt.Printf("Has context: %t\n", len(err.Context) > 0)
	fmt.Printf("Has suggestion: %t\n", err.Suggestion != "")
	fmt.Printf("Has position: %t\n", err.Position != nil)
	// Output:
	// Code: 1064
	// Message: Syntax error: invalid comparison operator
	// Has context: true
	// Has suggestion: true
	// Has position: true
}

// ExampleErrorPosition demonstrates error position formatting
func ExampleErrorPosition() {
	sql := "SELECT * FROM users WHERE id == 1"
	pos := &errno.ErrorPosition{
		Line:   1,
		Column: 30,
		Length: 2,
		SQL:    sql,
	}

	formatted := pos.Format()
	fmt.Print(formatted)
	// Output:
	//        SELECT * FROM users WHERE id == 1
	//                                     ^^
}

// ExampleNewStructuredError shows the constructor usage
func ExampleNewStructuredError() {
	err := errno.NewStructuredError(
		errno.ErrDupFieldName,
		"Duplicate column name 'id'",
	)

	fmt.Printf("Error code: %d\n", err.Code)
	fmt.Printf("Message: %s\n", err.Message)
	// Output:
	// Error code: 1060
	// Message: Duplicate column name 'id'
}

// ExampleStructuredError_methodChaining demonstrates fluent API usage
func ExampleStructuredError_methodChaining() {
	sql := "DELETE FROM users"

	// Create a comprehensive error using method chaining
	err := errno.NewStructuredError(
		errno.ErrParse,
		"Dangerous DELETE operation without WHERE clause",
	).
		WithContext("table", "users").
		WithContext("affected_rows", "all").
		WithSuggestion("Add a WHERE clause to limit deletion, or use TRUNCATE TABLE").
		WithPosition(1, 1, 6, sql)

	fmt.Printf("Error has all components: %t\n",
		err.Code > 0 &&
			len(err.Message) > 0 &&
			len(err.Context) > 0 &&
			len(err.Suggestion) > 0 &&
			err.Position != nil)
	// Output:
	// Error has all components: true
}
