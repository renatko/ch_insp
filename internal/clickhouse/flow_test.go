package clickhouse

import (
	"reflect"
	"testing"
)

func TestInferFlow(t *testing.T) {
	tests := []struct {
		name, sql, target string
		sources           []string
	}{
		{"join and qualified names", "CREATE MATERIALIZED VIEW db.mv TO db.output AS SELECT * FROM db.events JOIN other.users ON 1", "db.output", []string{"db.events", "other.users"}},
		{"internal storage", "CREATE MATERIALIZED VIEW mv ENGINE = MergeTree ORDER BY x AS SELECT * FROM source", "internal storage (no TO)", []string{"db.source"}},
		{"nested query and CTE", "CREATE MATERIALIZED VIEW mv TO out AS WITH x AS (SELECT * FROM raw) SELECT * FROM x JOIN (SELECT * FROM db.other) y ON 1", "db.out", []string{"db.raw", "db.other"}},
		{"comments literals and function", "CREATE MATERIALIZED VIEW mv TO `dest` AS SELECT 'FROM fake' FROM s3('x') JOIN `db`.`lookup` ON 1 -- FROM ignored", "db.dest", []string{"db.lookup"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := InferFlow(View{Database: "db", Definition: tt.sql})
			if flow.Target != tt.target || !reflect.DeepEqual(flow.Sources, tt.sources) {
				t.Fatalf("got %#v; want sources %v and target %q", flow, tt.sources, tt.target)
			}
		})
	}
}

func TestTriggerInferenceOnlyForSupportedDirectSource(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"CREATE MATERIALIZED VIEW mv TO dst AS SELECT a.id FROM db.events a JOIN db.orders b ON a.id=b.id", "db.events"},
		{"CREATE MATERIALIZED VIEW mv TO dst AS SELECT id FROM (SELECT id FROM db.events) q", ""},
		{"CREATE MATERIALIZED VIEW mv TO dst AS SELECT id FROM db.events UNION ALL SELECT id FROM db.orders", ""},
		{"CREATE MATERIALIZED VIEW mv REFRESH EVERY 1 MINUTE TO dst AS SELECT id FROM db.events", ""},
	}
	for _, tc := range cases {
		got := InferFlow(View{Database: "db", Definition: tc.sql})
		if got.Trigger != tc.want {
			t.Errorf("%s: trigger %q, want %q", tc.sql, got.Trigger, tc.want)
		}
	}
}
