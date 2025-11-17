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
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	// stringLiteralRe matches string literals in SQL queries
	stringLiteralRe = regexp.MustCompile(`'[^']*'`)
	// numberRe matches numeric literals in SQL queries
	numberRe = regexp.MustCompile(`\b\d+\b`)
	// whitespaceRe matches one or more whitespace characters
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// NormalizeQuery removes literals and normalizes SQL to create a query pattern.
// This function replaces string literals and numbers with placeholders to
// group similar queries together for sampling and aggregation.
//
// Examples:
//   SELECT * FROM products WHERE id = 123
//   -> SELECT * FROM products WHERE id = ?
//
//   SELECT * FROM users WHERE name = 'John' AND age = 25
//   -> SELECT * FROM users WHERE name = ? AND age = ?
func NormalizeQuery(sql string) string {
	// Replace string literals with ?
	sql = stringLiteralRe.ReplaceAllString(sql, "?")

	// Replace numbers with ?
	sql = numberRe.ReplaceAllString(sql, "?")

	// Normalize whitespace
	sql = whitespaceRe.ReplaceAllString(sql, " ")

	// Trim
	sql = strings.TrimSpace(sql)

	return sql
}

// PatternHash generates a short hash for a normalized query pattern.
// This hash is used to quickly identify and group identical query patterns.
// Returns a 16-character hexadecimal string.
func PatternHash(normalizedSQL string) string {
	hash := sha256.Sum256([]byte(normalizedSQL))
	return hex.EncodeToString(hash[:8]) // 16-char hex (8 bytes)
}
