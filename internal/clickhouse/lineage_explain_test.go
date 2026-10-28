package clickhouse

import (
	"strings"
	"testing"
)

func TestExplainResolvesCTEOutput(t *testing.T) {
	tree := `QUERY id: 0
  PROJECTION COLUMNS
    result UInt64
  PROJECTION
    LIST id: 1, nodes: 1
      COLUMN id: 2, column_name: v, result_type: UInt64, source_id: 3
  JOIN TREE
    QUERY id: 3, alias: __table1, is_subquery: 1
      PROJECTION COLUMNS
        v UInt64
      PROJECTION
        LIST id: 4, nodes: 1
          FUNCTION id: 5, function_name: plus, function_type: ordinary
            ARGUMENTS
              LIST id: 6, nodes: 2
                COLUMN id: 7, column_name: val, result_type: UInt64, source_id: 8
                CONSTANT id: 9, constant_value: UInt64_1
      JOIN TREE
        TABLE id: 8, alias: __table2, table_name: default.events`
	mappings := []ColumnMapping{{Output: "result"}}
	tables := []lineageTable{{Database: "default", Name: "events"}}
	meta := map[string][]tableColumn{"default.events": {{Name: "val", Type: "UInt64"}}}
	got := resolvedExplain(strings.Split(tree, "\n"), mappings, meta, tables)
	if len(got[0]) != 1 || got[0][0] != "events.val (UInt64)" {
		t.Fatalf("unexpected inputs: %#v", got)
	}
	if len(resolvedExplain(strings.Split(tree, "\n"), mappings, nil, tables)) != 0 {
		t.Fatal("missing metadata must not produce lineage")
	}
}

func TestExplainEligibility(t *testing.T) {
	for _, tc := range []struct {
		selectSQL string
		want      bool
	}{
		{"SELECT id + 1 AS x FROM db.events", true},
		{"SELECT upper(name) AS x FROM db.events", false},
		{"SELECT number FROM numbers(5)", false},
	} {
		p, err := parseLineage(View{Database: "db", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.dest AS " + tc.selectSQL})
		if err != nil {
			t.Fatal(err)
		}
		if got := explainEligible(p); got != tc.want {
			t.Errorf("%s: got %v", tc.selectSQL, got)
		}
	}
}
