package clickhouse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaRelationsConfiguredAndUnresolved(t *testing.T) {
	tests := []struct{ engine, full, ddl, other, detail string }{
		{"Distributed", "Distributed('prod', 'events', 'local', cityHash64(id))", "", "events.local", "cluster prod"},
		{"Distributed", "Distributed(getCluster(), 'events', 'local')", "", "", "unresolved"},
		{"Dictionary", "", "CREATE DICTIONARY db.d SOURCE(CLICKHOUSE(HOST 'localhost' DB 'db' TABLE 'src' PASSWORD 'secret'))", "db.src", "localhost"},
		{"Dictionary", "", "CREATE DICTIONARY db.d SOURCE(CLICKHOUSE(HOST 'other' DB 'db' TABLE 'src' PASSWORD 'secret'))", "", "external"},
		{"Dictionary", "", "CREATE DICTIONARY db.d SOURCE(HTTP(URL 'https://example.com/?token=secret'))", "", "HTTP"},
	}
	for _, tt := range tests {
		r := ParseSchemaRelation("db", "obj", tt.engine, tt.full, tt.ddl)
		if r.Other != tt.other || !strings.Contains(r.Detail, tt.detail) {
			t.Errorf("%s: got %#v", tt.engine, r)
		}
		if strings.Contains(r.Detail, "secret") {
			t.Fatal("credential exposed")
		}
	}
}

func TestServerTargetOverridesCachedSQLTarget(t *testing.T) {
	v := View{Database: "db", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.old AS SELECT * FROM db.src"}
	if got := InferFlow(v).Target; got != "db.old" {
		t.Fatal(got)
	}
	v.TargetDatabase, v.TargetTable = "db", "new"
	flow := InferFlow(v)
	if flow.Target != "db.new" || !strings.Contains(flow.TargetWarning, "db.old") {
		t.Fatalf("%+v", flow)
	}
	v.TargetTable = ""
	if got := InferFlow(v).Target; got != "db.old" {
		t.Fatal(got)
	}
}

func TestOfflineSchemaRelations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")
	ddl := `CREATE DATABASE db; USE db;
 CREATE TABLE local (id UInt64) ENGINE = MergeTree ORDER BY id;
 CREATE TABLE dist (id UInt64) ENGINE = Distributed('c', 'db', 'local', id);
 CREATE DICTIONARY dict (id UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(HOST 'localhost' DB 'db' TABLE 'local' PASSWORD 'secret')) LAYOUT(FLAT()) LIFETIME(60);`
	if err := os.WriteFile(path, []byte(ddl), 0600); err != nil {
		t.Fatal(err)
	}
	dump, err := LoadSQLDump(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(dump.Catalog.Relations) != 2 {
		t.Fatalf("relations: %#v warnings: %v", dump.Catalog.Relations, dump.Warnings)
	}
	for _, rel := range dump.Catalog.Relations {
		if rel.Other != "db.local" {
			t.Fatalf("%+v", rel)
		}
	}
}
