package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ch_insp/internal/clickhouse"
)

func TestImpactNavigationAndOfflineColumns(t *testing.T) {
	sql := `CREATE DATABASE db;
    CREATE TABLE db.src (id UInt64, status String) ENGINE=MergeTree ORDER BY id;
    CREATE TABLE db.middle (renamed UInt64) ENGINE=MergeTree ORDER BY renamed;
    CREATE TABLE db.dst (final UInt64) ENGINE=MergeTree ORDER BY final;
    CREATE MATERIALIZED VIEW db.first TO db.middle AS SELECT id AS renamed FROM db.src WHERE status = 'ok';
    CREATE MATERIALIZED VIEW db.second TO db.dst AS SELECT renamed AS final FROM db.middle;`
	path := filepath.Join(t.TempDir(), "schema.sql")
	if err := os.WriteFile(path, []byte(sql), 0600); err != nil {
		t.Fatal(err)
	}
	dump, err := clickhouse.LoadSQLDump(path, "db")
	if err != nil {
		t.Fatal(err)
	}
	m := model{dsn: "offline:", schemaPath: path, dump: dump, width: 120, height: 30, database: "db", views: dump.Catalog.Views, allViews: dump.Catalog.Views, dependencies: true, dependencyTable: "db.src"}
	next, cmd := m.Update(mouseKey("i"))
	m = next.(model)
	if !m.impact || !m.impactLoading || cmd == nil {
		t.Fatal("table impact did not start")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.impactLoading || !strings.Contains(strings.Join(m.impactLines(), "\n"), "db.dst") {
		t.Fatalf("table impact missing downstream: %+v", m.impactResult)
	}
	next, _ = m.Update(mouseKey("/"))
	m = next.(model)
	next, _ = m.Update(mouseKey("dst"))
	m = next.(model)
	if got := strings.Join(m.impactLines(), "\n"); !strings.Contains(got, "db.dst") || !strings.Contains(got, "db.first") {
		t.Fatalf("filter lost matching path and ancestors: %s", got)
	}
	next, _ = m.Update(mouseKey("enter"))
	m = next.(model)
	if m.impactFiltering {
		t.Fatal("filter edit did not end")
	}
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if m.impact || !m.dependencies {
		t.Fatal("did not return to dependencies")
	}
	next, cmd = m.Update(mouseKey("t"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("table columns did not load")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.targetError != nil || len(m.targetData.Columns) != 2 {
		t.Fatalf("target metadata: %#v %v", m.targetData, m.targetError)
	}
	next, cmd = m.Update(mouseKey("i"))
	m = next.(model)
	if !m.impact || m.impactColumn != "id" || cmd == nil {
		t.Fatalf("column impact did not start: %q", m.impactColumn)
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if got := strings.Join(m.impactLines(), "\n"); !strings.Contains(got, "db.dst.final") {
		t.Fatalf("column path missing: %s", got)
	}
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if !m.target || m.dependencies || m.impact {
		t.Fatal("did not return to target columns")
	}
}

func TestImpactIgnoresStaleResult(t *testing.T) {
	m := model{width: 120, height: 30, dependencies: true, impact: true, columnsRequest: 3, viewRevision: 4, impactLoading: true, views: []clickhouse.View{{Name: "mv"}}}
	next, _ := m.Update(impactLoadedMsg{request: 2, revision: 4, result: clickhouse.ImpactResult{Warnings: []string{"stale"}}})
	got := next.(model)
	if !got.impactLoading || len(got.impactResult.Warnings) != 0 {
		t.Fatal("stale result applied")
	}
}
