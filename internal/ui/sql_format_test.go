package ui

import (
	"errors"
	"strings"
	"testing"

	"ch_insp/internal/clickhouse"
	parser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

func TestFormatSQLPreservesQuotedValuesAndOriginal(t *testing.T) {
	original := "CREATE MATERIALIZED VIEW mv TO dst AS SELECT 'FROM, WHERE' AS text, count() AS n FROM src WHERE text = 'a b' GROUP BY text"
	formatted, ok := formatSQL(original)
	if !ok || !strings.Contains(formatted, "\nSELECT") || !strings.Contains(formatted, "\nFROM") || !strings.Contains(formatted, "'FROM, WHERE'") || !strings.Contains(formatted, "'a b'") {
		t.Fatalf("unexpected formatted SQL: %q (ok %v)", formatted, ok)
	}
	m := model{definition: true, views: []clickhouse.View{{Name: "mv", Definition: original}}}
	next, _ := m.Update(mouseKey("v"))
	m = next.(model)
	if !m.sqlOriginal || !strings.Contains(strings.Join(m.detailLines(), "\n"), original) {
		t.Fatal("original SQL is not available")
	}
	unsupported := "CREATE MATERIALIZED VIEW ???"
	if got, ok := formatSQL(unsupported); ok || got != unsupported {
		t.Fatalf("unsupported SQL did not preserve original: %q", got)
	}
}

func TestFormattedSQLParsesLikeOriginal(t *testing.T) {
	original := "CREATE MATERIALIZED VIEW mv TO dst AS SELECT 'where, from' AS x, count() AS n /* SELECT, FROM */ FROM src WHERE x = 'a\\'b' GROUP BY x"
	formatted, ok := formatSQL(original)
	if !ok {
		t.Fatal("SQL parser did not accept the test definition")
	}
	before, err := parser.NewParser(original).ParseStmts()
	if err != nil {
		t.Fatal(err)
	}
	after, err := parser.NewParser(formatted).ParseStmts()
	if err != nil || len(after) != 1 || before[0].String() != after[0].String() || !strings.Contains(formatted, "/* SELECT, FROM */") {
		t.Fatalf("format changed parsed SQL or comment: %q, %v", formatted, err)
	}
}

func TestDetailErrorKeepsOriginalMessage(t *testing.T) {
	message := "ClickHouse exception: first line\nsecond line with details"
	m := model{views: []clickhouse.View{{Name: "mv"}}, columns: true, columnsError: errors.New(message)}
	lines := strings.Join(m.detailLines(), "\n")
	if !strings.Contains(lines, message) {
		t.Fatalf("original error was changed: %q", lines)
	}
}
