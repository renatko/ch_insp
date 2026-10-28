package clickhouse

import (
	"strings"
	"testing"
)

func lineageFixture() map[string][]tableColumn {
	return map[string][]tableColumn{
		"db.events": {{Name: "id", Type: "UInt64"}, {Name: "name", Type: "String"}, {Name: "created_at", Type: "DateTime"}},
		"db.orders": {{Name: "id", Type: "UInt64"}, {Name: "price", Type: "UInt32"}},
		"db.mv":     {{Name: "id", Type: "UInt64"}, {Name: "upper_name", Type: "String"}, {Name: "total", Type: "UInt64"}},
		"db.out":    {{Name: "id", Type: "UInt64"}, {Name: "upper_name", Type: "String"}, {Name: "total", Type: "UInt64"}, {Name: "extra", Type: "String", DefaultKind: "DEFAULT", DefaultExpression: "'x'"}},
	}
}

func TestColumnLineageSelectJoinAndTargetDefaults(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT e.id, upper(e.name) AS upper_name, sum(o.price) AS total FROM db.events e JOIN db.orders o ON e.id = o.id WHERE e.created_at > now() GROUP BY e.id, e.name"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if len(result.Columns) != 3 || len(plan.sources) != 2 {
		t.Fatalf("unexpected lineage: %#v", result)
	}
	for i, want := range []struct{ out, expr, input, typ string }{{"id", "e.id", "events.id (UInt64)", "UInt64"}, {"upper_name", "upper(e.name)", "events.name (String)", "String"}, {"total", "sum(o.price)", "orders.price (UInt32)", "UInt64"}} {
		got := result.Columns[i]
		if got.Output != want.out || got.Expression != want.expr || len(got.Inputs) != 1 || got.Inputs[0] != want.input || got.TargetType != want.typ || got.TargetStatus != "matched by name" {
			t.Fatalf("projection %d: %#v", i, got)
		}
	}
	joined := strings.Join(result.AdditionalReads, " | ")
	for _, want := range []string{"events.id (UInt64)", "orders.id (UInt64)", "events.created_at (DateTime)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing additional reference %q: %v", want, result.AdditionalReads)
		}
	}
	if len(result.TargetExtras) != 1 || !strings.Contains(result.TargetExtras[0], "DEFAULT 'x'") {
		t.Fatalf("missing target default: %v", result.TargetExtras)
	}
}

func TestColumnLineageWildcardAndAmbiguousReferences(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT *, CAST(id AS UInt32) AS n FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.mv"] = []tableColumn{{Name: "id", Type: "UInt64"}, {Name: "name", Type: "String"}, {Name: "created_at", Type: "DateTime"}, {Name: "n", Type: "UInt32"}}
	result := analyzeColumns(view, plan, metadata, nil)
	if len(result.Columns) != 4 || result.Columns[0].Output != "id" || result.Columns[1].Output != "name" || result.Columns[3].Inputs[0] != "events.id (UInt64)" {
		t.Fatalf("wildcard or cast not resolved: %#v", result)
	}
	view.Definition = "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT id FROM db.events e JOIN db.orders o ON e.id=o.id"
	plan, err = parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result = analyzeColumns(view, plan, lineageFixture(), nil)
	if len(result.Columns) != 1 || len(result.Columns[0].Inputs) != 1 || !strings.Contains(result.Columns[0].Inputs[0], "ambiguous") {
		t.Fatalf("ambiguous id was assigned: %#v", result)
	}
}

func TestColumnLineageComplexAndMissingMetadata(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT id FROM db.events) SELECT id FROM data"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if len(result.Warnings) == 0 || len(result.Columns) != 1 || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "unresolved") {
		t.Fatalf("complex SQL must be marked unresolved: %#v", result)
	}
	view.Definition = "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT missing AS value FROM db.events"
	plan, err = parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result = analyzeColumns(view, plan, nil, nil)
	if result.Columns[0].TargetStatus != "target metadata unavailable" || !strings.Contains(result.Columns[0].Inputs[0], "unresolved") {
		t.Fatalf("missing metadata should be explicit: %#v", result)
	}
}

func TestColumnLineageParsesClickHouseStoredDefinition(t *testing.T) {
	view := View{Database: "default", Name: "mv", Definition: "CREATE MATERIALIZED VIEW default.`mv` TO default.dst (`id` UInt64, `upper_name` String) AS SELECT id, upper(name) AS upper_name FROM default.src"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	if plan.target == nil || plan.target.key() != "default.dst" || len(plan.sources) != 1 || plan.sources[0].key() != "default.src" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestUnqualifiedColumnIsNotAttributedWithMissingSourceMetadata(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT id, e.name FROM db.events e JOIN db.orders o ON e.id=o.id"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	delete(metadata, "db.orders")
	result := analyzeColumns(view, plan, metadata, nil)
	if !strings.Contains(result.Columns[0].Inputs[0], "unresolved: source metadata incomplete") {
		t.Fatalf("incorrect attribution: %#v", result.Columns[0])
	}
	if result.Columns[1].Inputs[0] != "events.name (String)" {
		t.Fatalf("qualified reference should resolve: %#v", result.Columns[1])
	}
}

func TestWildcardUsesViewSchemaAndRequiresMetadata(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT * FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.events"] = append(metadata["db.events"], tableColumn{Name: "hidden", Type: "String", DefaultKind: "ALIAS"})
	metadata["db.mv"] = []tableColumn{{Name: "id", Type: "UInt64"}, {Name: "name", Type: "String"}}
	result := analyzeColumns(view, plan, metadata, nil)
	if len(result.Columns) != 2 || result.Columns[0].Output != "id" || result.Columns[1].Output != "name" {
		t.Fatalf("wildcard included columns absent from MV metadata: %#v", result.Columns)
	}
	delete(metadata, "db.mv")
	result = analyzeColumns(view, plan, metadata, nil)
	if len(result.Columns) != 1 || !strings.Contains(result.Columns[0].Output, "unresolved") || len(result.Warnings) == 0 {
		t.Fatalf("missing view metadata should not invent outputs: %#v", result)
	}
}

func TestUnaliasedExpressionOutputNameRequiresMetadata(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT count() FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, nil, nil)
	if len(result.Columns) != 1 || result.Columns[0].TargetStatus != "output name unresolved; inspect SQL" {
		t.Fatalf("unverified name should be explicit: %#v", result)
	}
}

func TestOutputMissingFromMVSchemaIsNotPresentedAsTargetMatch(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT name AS upper_name FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.mv"] = []tableColumn{{Name: "different", Type: "String"}}
	result := analyzeColumns(view, plan, metadata, nil)
	if result.Columns[0].TargetStatus != "not found in MV schema; inspect SQL" || result.Columns[0].TargetType != "" {
		t.Fatalf("target match should remain uncertain: %#v", result.Columns[0])
	}
}

func TestLambdaParameterIsNotAttributedToSourceColumn(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT arrayMap(x -> x + 1, arr) AS result FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.events"] = []tableColumn{{Name: "x", Type: "UInt64"}, {Name: "arr", Type: "Array(UInt64)"}}
	result := analyzeColumns(view, plan, metadata, nil)
	if len(result.Warnings) == 0 {
		t.Fatal("lambda requires explicit uncertainty")
	}
	for _, input := range result.Columns[0].Inputs {
		if strings.Contains(input, "events.x (") {
			t.Fatalf("lambda parameter was treated as a source: %#v", result.Columns[0])
		}
	}
}
