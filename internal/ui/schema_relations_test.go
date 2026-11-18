package ui

import (
	"ch_insp/internal/clickhouse"
	"strings"
	"testing"
)

func TestDependenciesShowConfiguredRelations(t *testing.T) {
	m := model{dependencyTable: "db.src", schemaRelations: []clickhouse.SchemaRelation{
		{Database: "db", Name: "dist", Kind: "Distributed", Other: "db.src", Detail: "cluster c; remote table (not verified)"},
		{Database: "db", Name: "dict", Kind: "Dictionary", Other: "db.src", Detail: "ClickHouse source on localhost (configured)"},
	}}
	items := m.dependencyItems()
	if len(items) != 2 || items[0].table != "db.dist" || items[1].table != "db.dict" || !strings.Contains(items[1].label, "loads from") {
		t.Fatalf("%+v", items)
	}
	m.dependencyTable = "db.dict"
	items = m.dependencyItems()
	if len(items) != 1 || items[0].table != "db.src" {
		t.Fatalf("%+v", items)
	}
}

func TestSchemaObjectPickerWorksWithoutViews(t *testing.T) {
	m := model{overview: true, schemaRelations: []clickhouse.SchemaRelation{{Database: "db", Name: "dict", Kind: "Dictionary", Detail: "unknown source"}}}
	m.openSchemaObjects()
	if !m.choosingDependency || len(m.dependencyChoices) != 1 || m.dependencyChoices[0] != "db.dict" {
		t.Fatalf("%+v", m.dependencyChoices)
	}
}
