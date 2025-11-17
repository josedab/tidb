# Structured Error Message Framework

This document describes the structured error framework introduced to improve error messages in TiDB.

## Overview

The structured error framework provides rich, context-aware error messages that help users quickly identify and fix issues. Instead of vague error messages, users receive:

- **Precise error location** with visual indicators
- **Contextual information** about what went wrong
- **Actionable suggestions** for how to fix the problem
- **Relevant diagnostic data** (table names, column names, values, etc.)

## Motivation

**Before:**
```sql
mysql> SELECT * FROM users WHERE id == 1;
ERROR 1064: You have an error in your SQL syntax
```

**After:**
```sql
mysql> SELECT * FROM users WHERE id == 1;
ERROR 1064: Syntax error: invalid comparison operator
  operator: ==
  expected: =
Suggestion: Use '=' for comparison instead of '=='
       SELECT * FROM users WHERE id == 1;
                                    ^^
```

## API Reference

### StructuredError

The main error type that provides rich error information:

```go
type StructuredError struct {
    Code       uint16                 // MySQL error code
    Message    string                 // Human-readable error message
    Context    map[string]interface{} // Diagnostic data
    Suggestion string                 // Actionable fix suggestion
    Position   *ErrorPosition         // Location in SQL
}
```

### ErrorPosition

Pinpoints the exact location of an error in SQL text:

```go
type ErrorPosition struct {
    Line   int    // Line number (1-indexed)
    Column int    // Column number (1-indexed)
    Length int    // Length of the error token
    SQL    string // The complete SQL statement
}
```

## Usage Examples

### Basic Usage

```go
import "github.com/pingcap/tidb/pkg/errno"

// Create a simple structured error
err := errno.NewStructuredError(
    errno.ErrParse,
    "Syntax error near '=='",
)
```

### With Context

Add diagnostic information to help users understand what went wrong:

```go
err := errno.NewStructuredError(errno.ErrBadTable, "Table not found").
    WithContext("table", "user").
    WithContext("database", "mydb").
    WithContext("similar_tables", []string{"users", "user_info"})
```

### With Suggestions

Provide actionable advice for fixing the error:

```go
err := errno.NewStructuredError(errno.ErrParse, "Invalid operator").
    WithSuggestion("Use '=' for comparison instead of '=='")
```

### With Position

Show exactly where the error occurred in the SQL:

```go
sql := "SELECT * FROM users WHERE id == 1"
err := errno.NewStructuredError(errno.ErrParse, "Syntax error").
    WithPosition(1, 29, 2, sql)
```

### Complete Example

Combining all features:

```go
func parseWhereClause(sql string, line, col int) error {
    // ... parsing logic ...

    if foundInvalidOperator {
        return errno.NewStructuredError(
            errno.ErrParse,
            "Syntax error: invalid comparison operator",
        ).
        WithContext("operator", "==").
        WithContext("expected", "=").
        WithSuggestion("Use '=' for comparison instead of '=='").
        WithPosition(line, col, 2, sql)
    }

    return nil
}
```

## Real-World Examples

### Example 1: Table Not Found

```go
err := errno.NewStructuredError(
    errno.ErrBadTable,
    "Table 't' not found in database 'mydb'",
).
WithContext("table", "t").
WithContext("database", "mydb").
WithContext("similar_tables", []string{"tasks", "teams", "tests"}).
WithSuggestion("Did you mean 'tasks'?")

// Output:
// ERROR 1051: Table 't' not found in database 'mydb'
//   table: t
//   database: mydb
//   similar_tables: [tasks teams tests]
// Suggestion: Did you mean 'tasks'?
```

### Example 2: Duplicate Column

```go
sql := "SELECT id, name, id FROM users"
err := errno.NewStructuredError(
    errno.ErrDupFieldName,
    "Duplicate column name 'id'",
).
WithContext("column", "id").
WithContext("first_occurrence", "column 1").
WithContext("second_occurrence", "column 3").
WithPosition(1, 18, 2, sql).
WithSuggestion("Use column aliases to distinguish: SELECT id, name, id AS id2")

// Output:
// ERROR 1060: Duplicate column name 'id'
//   column: id
//   first_occurrence: column 1
//   second_occurrence: column 3
// Suggestion: Use column aliases to distinguish: SELECT id, name, id AS id2
//        SELECT id, name, id FROM users
//                         ^^
```

### Example 3: Type Mismatch

```go
sql := "SELECT * FROM users WHERE age = 'twenty'"
err := errno.NewStructuredError(
    errno.ErrBadField,
    "Type mismatch in WHERE clause",
).
WithContext("column", "age").
WithContext("column_type", "INT").
WithContext("provided_type", "STRING").
WithContext("provided_value", "twenty").
WithPosition(1, 33, 8, sql).
WithSuggestion("Provide a numeric value for column 'age'")

// Output:
// ERROR 1054: Type mismatch in WHERE clause
//   column: age
//   column_type: INT
//   provided_type: STRING
//   provided_value: twenty
// Suggestion: Provide a numeric value for column 'age'
//        SELECT * FROM users WHERE age = 'twenty'
//                                        ^^^^^^^^
```

## Best Practices

### 1. Use Descriptive Messages

**Good:**
```go
err := errno.NewStructuredError(
    errno.ErrParse,
    "Syntax error: invalid comparison operator '=='",
)
```

**Bad:**
```go
err := errno.NewStructuredError(errno.ErrParse, "invalid input")
```

### 2. Provide Relevant Context

Include information that helps diagnose the problem:

```go
err := errno.NewStructuredError(errno.ErrBadTable, "Table not found").
    WithContext("table", tableName).
    WithContext("database", dbName).
    WithContext("available_tables", getTables(dbName))
```

### 3. Make Suggestions Actionable

**Good:**
```go
WithSuggestion("Use '=' for comparison instead of '=='")
```

**Bad:**
```go
WithSuggestion("Fix your SQL syntax")
```

### 4. Use Position for Syntax Errors

Always include position information for syntax errors:

```go
err := errno.NewStructuredError(errno.ErrParse, "Unexpected token").
    WithPosition(line, column, tokenLength, sql)
```

### 5. Chain Method Calls

Use method chaining for cleaner code:

```go
return errno.NewStructuredError(errno.ErrParse, "Invalid operator").
    WithContext("operator", op).
    WithSuggestion("Use '=' instead").
    WithPosition(line, col, len(op), sql)
```

## Migration Guide

### Updating Existing Errors

To upgrade an existing error to use the structured format:

**Before:**
```go
return mysql.NewErr(errno.ErrParse, "syntax error")
```

**After:**
```go
return errno.NewStructuredError(errno.ErrParse, "Syntax error near '=='").
    WithSuggestion("Use '=' for comparison instead of '=='").
    WithPosition(line, col, 2, sql)
```

### Compatibility

StructuredError implements the standard `error` interface, so it can be used anywhere a regular error is expected:

```go
var err error = errno.NewStructuredError(errno.ErrParse, "syntax error")
fmt.Println(err.Error()) // Calls Format() automatically
```

## Testing

When testing code that uses StructuredError:

```go
func TestParseError(t *testing.T) {
    err := parseSQL("SELECT * FROM users WHERE id == 1")

    require.Error(t, err)

    structErr, ok := err.(*errno.StructuredError)
    require.True(t, ok, "Expected StructuredError")

    require.Equal(t, errno.ErrParse, structErr.Code)
    require.Contains(t, structErr.Message, "invalid operator")
    require.Equal(t, "==", structErr.Context["operator"])
    require.NotEmpty(t, structErr.Suggestion)
    require.NotNil(t, structErr.Position)
}
```

## Performance Considerations

- Creating StructuredError instances is lightweight
- Context map is lazily initialized
- Format() should only be called when displaying the error to users
- For internal errors that are never shown to users, consider using simpler error types

## Future Enhancements

Planned improvements to the structured error framework:

1. **Multi-line error highlighting** - Support for errors spanning multiple lines
2. **Error code documentation links** - Include URLs to detailed documentation
3. **Internationalization** - Support for localized error messages
4. **Error recovery hints** - Suggest alternative queries that might work
5. **Related errors** - Link to similar or related error messages

## References

- [RFC-0006: Structured Error Message Improvement Framework](../../analysis-output/rfcs/RFC-0006-error-message-framework.md)
- [PostgreSQL Error Codes](https://www.postgresql.org/docs/current/errcodes-appendix.html)
- [Rust Compiler Diagnostics](https://rustc-dev-guide.rust-lang.org/diagnostics.html)

## Contributing

When adding new error types or improving existing ones:

1. Use StructuredError for user-facing errors
2. Include context, suggestions, and position when applicable
3. Add tests for the error cases
4. Update this documentation with examples
5. Follow the best practices outlined above

For questions or suggestions, please open an issue in the TiDB repository.
