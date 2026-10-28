package clickhouse

import (
	"strings"
	"testing"
)

func TestTargetWritersGroupByTargetColumn(t *testing.T) {
	target := TargetColumns{Columns: []TargetColumn{{Name: "id", Type: "UInt64"}, {Name: "extra", Type: "String", DefaultKind: "DEFAULT", DefaultExpression: "'x'"}}}
	addTargetWriters(&target, "db.first", ColumnLineage{Columns: []ColumnMapping{{Output: "id", Expression: "e.id", ViewType: "UInt32", Inputs: []string{"events.id (UInt64)"}, TargetStatus: "matched by name"}, {Output: "unknown", TargetStatus: "output name unresolved; inspect SQL"}}})
	addTargetWriters(&target, "other.second", ColumnLineage{Columns: []ColumnMapping{{Output: "id", Expression: "o.id", Inputs: []string{"orders.id (UInt64)"}, TargetStatus: "matched by name"}}})
	if len(target.Columns[0].Writers) != 2 || target.Columns[0].Writers[1].View != "other.second" || len(target.Columns[1].Writers) != 0 {
		t.Fatalf("wrong grouping: %#v", target.Columns)
	}
	if target.Columns[0].Writers[0].ViewType != "UInt32" {
		t.Fatal("MV type not preserved")
	}
	if len(target.Warnings) != 1 || !strings.Contains(target.Warnings[0], "unknown") {
		t.Fatalf("uncertain mapping not reported: %v", target.Warnings)
	}
}
