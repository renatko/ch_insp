package ui

import (
	"strings"
	"unicode"

	parser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// formatSQL only inserts whitespace into parser-supported statements; quoted
// strings, identifiers and comments are copied verbatim. Unknown syntax stays
// in its original form so inspection never loses the server's definition.
func formatSQL(sql string) (string, bool) {
	statements, err := parser.NewParser(sql).ParseStmts()
	if err != nil || len(statements) != 1 {
		return sql, false
	}
	var out strings.Builder
	depth, selectList := 0, false
	last := byte(0)
	write := func(s string) {
		out.WriteString(s)
		if len(s) > 0 {
			last = s[len(s)-1]
		}
	}
	breakLine := func() {
		if last != 0 && last != '\n' && last != '\r' {
			write("\n")
		}
	}
	for i := 0; i < len(sql); {
		start := i
		if sql[i] == '\'' || sql[i] == '"' || sql[i] == '`' {
			quote := sql[i]
			i++
			for i < len(sql) {
				if sql[i] == '\\' && i+1 < len(sql) {
					i += 2
					continue
				}
				if sql[i] == quote {
					i++
					if i < len(sql) && sql[i] == quote {
						i++
						continue
					}
					break
				}
				i++
			}
			write(sql[start:i])
			continue
		}
		if i+1 < len(sql) && ((sql[i] == '-' && sql[i+1] == '-') || (sql[i] == '/' && sql[i+1] == '*')) {
			block := sql[i] == '/'
			i += 2
			if block {
				for i+1 < len(sql) && sql[i:i+2] != "*/" {
					i++
				}
				if i+1 < len(sql) {
					i += 2
				}
			} else {
				for i < len(sql) && sql[i] != '\n' {
					i++
				}
			}
			write(sql[start:i])
			continue
		}
		if (sql[i] >= 'A' && sql[i] <= 'Z') || (sql[i] >= 'a' && sql[i] <= 'z') || sql[i] == '_' {
			i++
			for i < len(sql) && (sql[i] == '_' || sql[i] >= '0' && sql[i] <= '9' || sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z') {
				i++
			}
			word := sql[start:i]
			if depth == 0 {
				switch strings.ToUpper(word) {
				case "SELECT":
					breakLine()
					selectList = true
				case "FROM":
					breakLine()
					selectList = false
				case "WHERE", "PREWHERE", "GROUP", "ORDER", "HAVING", "LIMIT", "SETTINGS", "UNION", "JOIN":
					breakLine()
				}
			}
			write(word)
			continue
		}
		if sql[i] == '(' {
			depth++
		} else if sql[i] == ')' && depth > 0 {
			depth--
		}
		write(sql[i : i+1])
		i++
		if sql[start] == ',' && depth == 0 && selectList {
			breakLine()
			write("  ")
		}
	}
	return strings.TrimRightFunc(out.String(), unicode.IsSpace), true
}
