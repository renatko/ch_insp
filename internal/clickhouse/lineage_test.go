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
	if len(result.Joins) != 1 || result.Joins[0] != "JOIN db.orders AS o ON e.id = o.id" || result.Where != "WHERE e.created_at > now()" || result.GroupBy != "GROUP BY e.id, e.name" {
		t.Fatalf("missing conditions: joins=%v where=%q group=%q", result.Joins, result.Where, result.GroupBy)
	}
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

func TestTopLevelConditionsMultipleJoinsAndNoClauses(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT a.id FROM a LEFT JOIN b ON a.id = b.id INNER JOIN c USING (id) PREWHERE a.id > 2 WHERE b.val > 0 GROUP BY a.id HAVING count() > 1"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	got := analyzeColumns(view, plan, nil, nil)
	if len(got.Joins) != 2 || got.Joins[0] != "LEFT JOIN b ON a.id = b.id" || got.Joins[1] != "INNER JOIN c USING id" || got.Prewhere != "PREWHERE a.id > 2" || got.Where != "WHERE b.val > 0" || got.GroupBy != "GROUP BY a.id" || got.Having != "HAVING count() > 1" {
		t.Fatalf("conditions missing: %#v", got)
	}
	view.Definition = "CREATE MATERIALIZED VIEW mv TO dst AS SELECT a FROM src"
	plan, err = parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	got = analyzeColumns(view, plan, nil, nil)
	if len(got.Joins) != 0 || got.Where != "" || got.GroupBy != "" || got.Prewhere != "" || got.Having != "" {
		t.Fatalf("invented conditions: %#v", got)
	}
}

func TestCommaJoinAppearsInConditions(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT a.id FROM a, b WHERE a.id = b.id"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	got := analyzeColumns(view, plan, nil, nil)
	if len(got.Joins) != 1 || got.Joins[0] != "COMMA JOIN b" || got.Where != "WHERE a.id = b.id" {
		t.Fatalf("comma join missing: %#v", got)
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
	if len(result.Columns) != 1 || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "events.id (UInt64)") {
		t.Fatalf("CTE lineage was not resolved: %#v", result)
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
	if len(result.Columns[0].Inputs) != 1 || result.Columns[0].Inputs[0] != "events.arr (Array(UInt64))" {
		t.Fatalf("lambda input should be traced: %#v", result.Columns[0])
	}
	for _, input := range result.Columns[0].Inputs {
		if strings.Contains(input, "events.x (") {
			t.Fatalf("lambda parameter was treated as a source: %#v", result.Columns[0])
		}
	}
}

func TestSelectAndWithAliasesTraceToPhysicalInputs(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH price * 2 AS doubled SELECT doubled + 1 AS final FROM db.orders"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	inputs := strings.Join(result.Columns[0].Inputs, " ")
	if !strings.Contains(inputs, "orders.price (UInt32)") || !strings.Contains(inputs, "via doubled = price * 2") {
		t.Fatalf("WITH alias not traced: %s", inputs)
	}
	view.Definition = "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT price * 2 AS doubled, doubled + 1 AS final FROM db.orders"
	plan, err = parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result = analyzeColumns(view, plan, lineageFixture(), nil)
	inputs = strings.Join(result.Columns[1].Inputs, " ")
	if !strings.Contains(inputs, "orders.price (UInt32)") || !strings.Contains(inputs, "via doubled = price * 2") {
		t.Fatalf("SELECT alias not traced: %s", inputs)
	}
}

func TestDerivedCTEAndFromSubqueryTraceTransformations(t *testing.T) {
	for _, definition := range []string{
		"CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT id, price * 2 AS cost FROM db.orders WHERE id > 0) SELECT cost AS total FROM data",
		"CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT q.cost AS total FROM (SELECT price * 2 AS cost FROM db.orders WHERE id > 0) AS q",
	} {
		view := View{Database: "db", Name: "mv", Definition: definition}
		plan, err := parseLineage(view)
		if err != nil {
			t.Fatal(err)
		}
		result := analyzeColumns(view, plan, lineageFixture(), nil)
		if len(plan.sources) != 1 || plan.sources[0].key() != "db.orders" {
			t.Fatalf("physical metadata not requested: %#v", plan.sources)
		}
		inputs := strings.Join(result.Columns[0].Inputs, " ")
		if !strings.Contains(inputs, "orders.price (UInt32)") || !strings.Contains(inputs, "via cost = price * 2") {
			t.Fatalf("derived input not traced: %s", inputs)
		}
		if !strings.Contains(strings.Join(result.AdditionalReads, " "), "orders.id (UInt64)") {
			t.Fatalf("nested WHERE column missing: %v", result.AdditionalReads)
		}
	}
}

func TestClickHouseStoredCTEAggregation(t *testing.T) {
	view := View{Database: "default", Name: "mv", Definition: "CREATE MATERIALIZED VIEW default.`mv` TO default.dst (`total` UInt64) AS WITH q AS (SELECT price * 2 AS cost FROM default.src WHERE id > 0) SELECT sum(cost) AS total FROM q"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string][]tableColumn{"default.src": {{Name: "id", Type: "UInt64"}, {Name: "price", Type: "UInt64"}}, "default.mv": {{Name: "total", Type: "UInt64"}}, "default.dst": {{Name: "total", Type: "UInt64"}}}
	result := analyzeColumns(view, plan, metadata, nil)
	if len(result.Columns) != 1 || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "src.price (UInt64) [via cost = price * 2]") || !strings.Contains(strings.Join(result.AdditionalReads, " "), "src.id (UInt64)") {
		t.Fatalf("stored CTE was not traced: %#v", result)
	}
}

func TestAliasCollisionAndCyclesStayUncertain(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT price * 2 AS id, id + 1 AS total FROM db.orders"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if !strings.Contains(strings.Join(result.Columns[1].Inputs, " "), "ambiguous") {
		t.Fatalf("alias and physical column collision should remain ambiguous: %#v", result.Columns[1])
	}
}

func TestLambdaShadowingDoesNotHideOuterColumn(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT arrayMap(x -> x + 1, arr) AS transformed, x AS actual FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.events"] = []tableColumn{{Name: "x", Type: "UInt64"}, {Name: "arr", Type: "Array(UInt64)"}}
	result := analyzeColumns(view, plan, metadata, nil)
	if strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "events.x (") || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "events.arr (") || !strings.Contains(strings.Join(result.Columns[1].Inputs, " "), "events.x (") {
		t.Fatalf("lambda scope wrong: %#v", result.Columns)
	}
}

func TestLambdaInExpressionDoesNotHideUnrelatedColumn(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT arrayMap(x -> x + 1, arr)[1] + x AS result FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.events"] = []tableColumn{{Name: "x", Type: "UInt64"}, {Name: "arr", Type: "Array(UInt64)"}}
	result := analyzeColumns(view, plan, metadata, nil)
	joined := strings.Join(result.Columns[0].Inputs, " | ")
	if !strings.Contains(joined, "events.arr (Array(UInt64))") || !strings.Contains(joined, "events.x (UInt64)") || strings.Contains(joined, "unresolved") {
		t.Fatalf("incorrect lambda scope: %s", joined)
	}
}

func TestLambdaParameterDoesNotHideSameNamedArrayArgument(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT arrayMap(x -> x + 1, x) AS result FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.events"] = []tableColumn{{Name: "x", Type: "Array(UInt64)"}}
	result := analyzeColumns(view, plan, metadata, nil)
	if len(result.Columns[0].Inputs) != 1 || result.Columns[0].Inputs[0] != "events.x (Array(UInt64))" {
		t.Fatalf("array argument was confused with lambda parameter: %#v", result.Columns[0])
	}
}

func TestCyclicAliasesRemainUnresolved(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH a AS b, b AS a SELECT b AS value FROM db.events"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if len(result.Columns) != 1 || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "cyclic alias") {
		t.Fatalf("cycle was not detected: %#v", result)
	}
}

func TestScalarSubqueryTracesOwnSourcesAndConditions(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT e.id + (SELECT sum(o.price) FROM db.orders o WHERE o.id > 0) AS total FROM db.events e"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if len(plan.sources) != 2 {
		t.Fatalf("scalar source metadata not requested: %#v", plan.sources)
	}
	inputs := strings.Join(result.Columns[0].Inputs, " | ")
	for _, want := range []string{"events.id (UInt64)", "orders.price (UInt32)", "orders.id (UInt64)", "via scalar subquery = sum(o.price)"} {
		if !strings.Contains(inputs, want) {
			t.Errorf("missing %q in %s", want, inputs)
		}
	}
	if strings.Contains(inputs, "unresolved") {
		t.Fatalf("uncorrelated scalar subquery should resolve: %s", inputs)
	}
}

func TestCorrelatedScalarSubqueryStaysUnresolved(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT (SELECT sum(o.price) FROM db.orders o WHERE o.id = e.id) AS total FROM db.events e"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	inputs := strings.Join(result.Columns[0].Inputs, " | ")
	if !strings.Contains(inputs, "orders.price (UInt32)") || !strings.Contains(inputs, "e.id (unresolved)") {
		t.Fatalf("correlation should remain explicit: %s", inputs)
	}
}

func TestDerivedJoinResolvesQualifiedOutputsToDifferentSources(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT a.id, b.id AS order_id FROM (SELECT id FROM db.events) a JOIN (SELECT id FROM db.orders) b ON a.id = b.id"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if len(result.Columns) != 2 || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "events.id (UInt64)") || !strings.Contains(strings.Join(result.Columns[1].Inputs, " "), "orders.id (UInt64)") {
		t.Fatalf("derived aliases were confused: %#v", result.Columns)
	}
}

func TestUnsupportedUnionRemainsExplicit(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT id FROM db.events UNION ALL SELECT id FROM db.orders"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Columns[0].Inputs, " "), "unsupported query scope") {
		t.Fatalf("UNION should not be presented as fully resolved: %#v", result)
	}
}

func TestDerivedWildcardResolvesSimplePhysicalSource(t *testing.T) {
	definitions := []string{
		"CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT * FROM db.events) SELECT id FROM data",
		"CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT q.id FROM (SELECT e.* FROM db.events e) q",
	}
	for _, definition := range definitions {
		view := View{Database: "db", Name: "mv", Definition: definition}
		plan, err := parseLineage(view)
		if err != nil {
			t.Fatal(err)
		}
		result := analyzeColumns(view, plan, lineageFixture(), nil)
		input := strings.Join(result.Columns[0].Inputs, " ")
		if !strings.Contains(input, "events.id (UInt64)") || strings.Contains(input, "ambiguous") || strings.Contains(input, "unresolved") {
			t.Fatalf("wildcard was not resolved: %s", input)
		}
	}
}

func TestDerivedWildcardExplainsIncompleteMetadata(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT * FROM db.events) SELECT id FROM data"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	delete(metadata, "db.events")
	result := analyzeColumns(view, plan, metadata, nil)
	input := strings.Join(result.Columns[0].Inputs, " ")
	if !strings.Contains(input, "source metadata incomplete for db.events") || !strings.Contains(input, "system.columns") || strings.Contains(input, "ambiguous or metadata incomplete") {
		t.Fatalf("missing actionable reason: %s", input)
	}
}

func TestDerivedWildcardWithMultipleSourcesIsNotGuessed(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT * FROM db.events e JOIN db.orders o ON e.id = o.id) SELECT id FROM data"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	input := strings.Join(result.Columns[0].Inputs, " ")
	if !strings.Contains(input, "wildcard spans multiple or unknown inputs") {
		t.Fatalf("wildcard should not be guessed: %s", input)
	}
}

func TestDerivedWildcardAndExplicitOutputWithoutCollision(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT *, upper(name) AS upper_name FROM db.events) SELECT upper_name FROM data"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	input := strings.Join(result.Columns[0].Inputs, " ")
	if !strings.Contains(input, "events.name (String)") || strings.Contains(input, "unresolved") || strings.Contains(input, "ambiguous") {
		t.Fatalf("explicit output should resolve: %s", input)
	}
}

func TestAliasWithMissingMetadataReportsMissingTable(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS SELECT price * 2 AS doubled, doubled + 1 AS total FROM db.orders"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	delete(metadata, "db.orders")
	result := analyzeColumns(view, plan, metadata, nil)
	input := strings.Join(result.Columns[1].Inputs, " ")
	if !strings.Contains(input, "source metadata incomplete for db.orders") || strings.Contains(input, "ambiguous alias or source") {
		t.Fatalf("metadata error confused with ambiguity: %s", input)
	}
}

func TestDerivedWildcardDoesNotGuessDuplicateOutput(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT *, upper(name) AS name FROM db.events) SELECT name FROM data"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	result := analyzeColumns(view, plan, lineageFixture(), nil)
	input := strings.Join(result.Columns[0].Inputs, " ")
	if !strings.Contains(input, "explicit output and wildcard may both produce name") {
		t.Fatalf("duplicate output should remain uncertain: %s", input)
	}
}

func TestDerivedWildcardOnAliasColumnIsSettingsDependent(t *testing.T) {
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.out AS WITH data AS (SELECT * FROM db.events) SELECT derived FROM data"}
	plan, err := parseLineage(view)
	if err != nil {
		t.Fatal(err)
	}
	metadata := lineageFixture()
	metadata["db.events"] = append(metadata["db.events"], tableColumn{Name: "derived", Type: "UInt64", DefaultKind: "ALIAS"})
	result := analyzeColumns(view, plan, metadata, nil)
	input := strings.Join(result.Columns[0].Inputs, " ")
	if !strings.Contains(input, "wildcard inclusion of ALIAS columns depends on ClickHouse settings") {
		t.Fatalf("settings-dependent wildcard should remain uncertain: %s", input)
	}
}
