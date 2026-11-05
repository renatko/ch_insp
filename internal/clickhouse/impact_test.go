package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestTableImpactBranchesAndCycles(t *testing.T) {
	views := []View{
		{Database: "db", Name: "first", Definition: "CREATE MATERIALIZED VIEW db.first TO db.middle AS SELECT id FROM db.source"},
		{Database: "db", Name: "second", Definition: "CREATE MATERIALIZED VIEW db.second TO db.end AS SELECT id FROM db.middle"},
		{Database: "db", Name: "branch", Definition: "CREATE MATERIALIZED VIEW db.branch TO db.other AS SELECT id FROM db.middle"},
		{Database: "db", Name: "loop", Definition: "CREATE MATERIALIZED VIEW db.loop TO db.source AS SELECT id FROM db.middle"},
	}
	got := AnalyzeImpact(context.Background(), "db.source", "", views, nil)
	var text strings.Builder
	cycles := 0
	for _, item := range got.Items {
		fmt.Fprintf(&text, "%s %s ", item.View.Name, item.Table)
		if item.Cycle {
			cycles++
		}
	}
	for _, name := range []string{"first", "second", "branch", "loop", "db.end", "db.other"} {
		if !strings.Contains(text.String(), name) {
			t.Errorf("missing %s: %s", name, text.String())
		}
	}
	if cycles != 1 || got.Truncated {
		t.Fatalf("cycles=%d result=%+v", cycles, got)
	}
}

func TestColumnImpactFollowsNamesAndConditions(t *testing.T) {
	metadata := map[string][]tableColumn{
		"db.source": {{Name: "id", Type: "UInt64"}, {Name: "status", Type: "String"}},
		"db.lookup": {{Name: "id", Type: "UInt64"}},
		"db.first":  {{Name: "renamed", Type: "UInt64"}},
		"db.middle": {{Name: "renamed", Type: "UInt64"}},
		"db.second": {{Name: "final", Type: "UInt64"}},
		"db.dest":   {{Name: "final", Type: "UInt64"}},
	}
	views := []View{
		{Database: "db", Name: "first", Definition: "CREATE MATERIALIZED VIEW db.first TO db.middle AS SELECT s.id AS renamed FROM db.source s JOIN db.lookup l ON s.id = l.id WHERE s.status = 'ok'"},
		{Database: "db", Name: "second", Definition: "CREATE MATERIALIZED VIEW db.second TO db.dest AS SELECT renamed AS final FROM db.middle"},
	}
	loader := func(_ context.Context, view View) (ColumnLineage, error) {
		plan, err := parseLineage(view)
		if err != nil {
			return ColumnLineage{}, err
		}
		return analyzeColumns(view, plan, metadata, nil), nil
	}
	result := AnalyzeImpact(context.Background(), "db.source", "id", views, loader)
	var selected, joined, propagated bool
	for _, item := range result.Items {
		if item.View.Name == "first" && item.Usage == "SELECT" && item.Expression == "s.id" {
			selected = true
		}
		if item.View.Name == "first" && item.Usage == "JOIN" {
			joined = true
		}
		if item.Table == "db.dest" && item.Column == "final" && item.Status == "target" {
			propagated = true
		}
	}
	if !selected || !joined || !propagated {
		t.Fatalf("missing select=%v join=%v propagated=%v: %#v", selected, joined, propagated, result)
	}
	result = AnalyzeImpact(context.Background(), "db.source", "status", views, loader)
	for _, item := range result.Items {
		if item.Usage == "WHERE" {
			return
		}
	}
	t.Fatalf("WHERE use missing: %#v", result)
}

func TestColumnImpactDoesNotInventTargetOrAmbiguousSource(t *testing.T) {
	views := []View{{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW db.mv TO db.target AS SELECT id AS out FROM db.source s JOIN db.other o ON s.id=o.id"}}
	metadata := map[string][]tableColumn{"db.source": {{Name: "id", Type: "UInt64"}}, "db.other": {{Name: "id", Type: "UInt64"}}, "db.mv": {{Name: "out", Type: "UInt64"}}, "db.target": {{Name: "different", Type: "UInt64"}}}
	loader := func(_ context.Context, v View) (ColumnLineage, error) {
		p, e := parseLineage(v)
		if e != nil {
			return ColumnLineage{}, e
		}
		return analyzeColumns(v, p, metadata, nil), nil
	}
	result := AnalyzeImpact(context.Background(), "db.source", "id", views, loader)
	possible := false
	for _, edge := range result.Items {
		if edge.Status == "target" && edge.Column != "" {
			t.Fatalf("invented target: %#v", result)
		}
		if edge.Status == "possible" {
			possible = true
		}
	}
	if !possible {
		t.Fatalf("ambiguity not reported: %#v", result)
	}
}
