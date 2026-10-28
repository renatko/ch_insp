package clickhouse

import "testing"

func TestTableDependenciesIncludeReadersWritersAndCycles(t *testing.T) {
	views := []View{{Database: "db", Name: "a", Definition: "CREATE MATERIALIZED VIEW a TO b AS SELECT * FROM a"}, {Database: "db", Name: "b", Definition: "CREATE MATERIALIZED VIEW b TO a AS SELECT * FROM b"}, {Database: "other", Name: "c", Definition: "CREATE MATERIALIZED VIEW c TO db.b AS SELECT * FROM db.a"}}
	deps := TableDependencies("db.a", views)
	if len(deps) != 3 {
		t.Fatalf("unexpected dependencies: %#v", deps)
	}
	var writers, readers int
	for _, d := range deps {
		if d.Direction == "writes" {
			writers++
		} else if d.Direction == "reads" {
			readers++
			if d.Role != "inferred trigger" {
				t.Fatalf("wrong role: %#v", d)
			}
		}
	}
	if writers != 1 || readers != 2 {
		t.Fatalf("wrong directions: %#v", deps)
	}
}

func TestCommaJoinDependencyIncludesEachInput(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO out AS SELECT a.id FROM db.a a, db.b b"}
	deps := TableDependencies("db.b", []View{view})
	if len(deps) != 1 || deps[0].Direction != "reads" || deps[0].Other != "db.out" {
		t.Fatalf("missing comma-join input: %#v", deps)
	}
}
