package clickhouse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLDumpCatalogLineageAndTargets(t *testing.T) {
	sql := `-- a semicolon in a comment ;
CREATE DATABASE IF NOT EXISTS shop;
USE shop;
CREATE TABLE events (user_id UInt64, category String DEFAULT 'a;b', amount Float64) ENGINE = MergeTree ORDER BY user_id;
CREATE TABLE results (user_id UInt64, total Float64) ENGINE = MergeTree ORDER BY user_id;
CREATE MATERIALIZED VIEW totals TO results AS SELECT user_id, sum(amount) AS total FROM events WHERE category != 'x;y' GROUP BY user_id;
CREATE DATABASE other;
CREATE TABLE other.events (user_id UInt64) ENGINE=Null;
CREATE MATERIALIZED VIEW other.audit TO shop.results AS SELECT user_id, toFloat64(user_id) AS total FROM other.events;
DROP TABLE shop.ignored;`
	path := filepath.Join(t.TempDir(), "schema.sql")
	if err := os.WriteFile(path, []byte(sql), 0600); err != nil {
		t.Fatal(err)
	}
	dump, err := LoadSQLDump(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(dump.Catalog.Views) != 2 || len(dump.Catalog.Databases) != 2 {
		t.Fatalf("bad catalog: %#v warnings=%v", dump.Catalog, dump.Warnings)
	}
	if len(dump.Warnings) == 0 {
		t.Fatal("unsupported statement was not reported")
	}
	var selected View
	for _, v := range dump.Catalog.Views {
		if v.Name == "totals" {
			selected = v
		}
	}
	if selected.Database != "shop" {
		t.Fatalf("USE not respected: %#v", selected)
	}
	lineage, err := dump.ColumnLineage(selected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lineage.Where, "x;y") || !strings.Contains(lineage.GroupBy, "user_id") || len(lineage.Columns) != 2 {
		t.Fatalf("bad lineage: %#v", lineage)
	}
	result, err := dump.TargetColumns("shop", "results", dump.Catalog.Views)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 2 || len(result.Views) != 2 || len(result.Columns[0].Writers) != 2 {
		t.Fatalf("bad target: %#v", result)
	}
	if _, err := dump.TargetColumns("shop", "missing", nil); err == nil {
		t.Fatal("missing metadata not reported")
	}
}
func TestSQLDumpSplittingAndInvalidInput(t *testing.T) {
	parts, err := splitDumpStatements("SELECT 'a;''b'; /* ; */ SELECT `a;b`; -- ;\nSELECT 3;")
	if err != nil || len(parts) != 3 {
		t.Fatalf("split: %#v %v", parts, err)
	}
	if _, err = splitDumpStatements("SELECT 'unterminated"); err == nil {
		t.Fatal("unterminated quote accepted")
	}
}

func TestSQLDumpAllowsSchemaWithoutViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sql")
	if err := os.WriteFile(path, []byte("CREATE DATABASE empty; CREATE TABLE empty.events (id UInt64) ENGINE=Null;"), 0600); err != nil {
		t.Fatal(err)
	}
	dump, err := LoadSQLDump(path, "")
	if err != nil || len(dump.Catalog.Views) != 0 || dump.Catalog.CurrentDatabase != "empty" {
		t.Fatalf("empty schema: %#v %v", dump, err)
	}
}
