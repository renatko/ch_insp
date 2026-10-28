package ui

import (
	"strings"
	"testing"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func mouseAtText(t *testing.T, m model, text string) tea.MouseMsg {
	t.Helper()
	for y, line := range strings.Split(m.View(), "\n") {
		plain := ansi.Strip(line)
		if i := strings.Index(plain, text); i >= 0 {
			return tea.MouseMsg{X: lipgloss.Width(plain[:i]), Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
		}
	}
	t.Fatalf("could not find %q in screen:\n%s", text, m.View())
	return tea.MouseMsg{}
}

func TestMouseSelectsViewAndOpensOverview(t *testing.T) {
	m := model{width: 120, height: 25, metricsBusy: true, views: []clickhouse.View{{Name: "first"}, {Name: "second"}}}
	next, _ := m.Update(mouseAtText(t, m, "second"))
	m = next.(model)
	if m.selected != 1 {
		t.Fatalf("selected view = %d", m.selected)
	}
	next, _ = m.Update(mouseAtText(t, m, "[o]"))
	if !next.(model).overview {
		t.Fatal("overview did not open")
	}
}

func TestMouseDatabaseSelection(t *testing.T) {
	m := model{width: 100, height: 25, choosingDatabase: true, database: "one", databases: []string{"one", "two"}, metricsBusy: true, allViews: []clickhouse.View{{Database: "two", Name: "mv"}}}
	next, _ := m.Update(mouseAtText(t, m, "two"))
	m = next.(model)
	if m.choosingDatabase || m.database != "two" || len(m.views) != 1 {
		t.Fatalf("database selection failed: %#v", m)
	}
}

func TestMouseOverviewSelectionAndOpen(t *testing.T) {
	m := model{width: 120, height: 30, overview: true, metricsBusy: true, views: []clickhouse.View{
		{Database: "db", Name: "one", Definition: "CREATE MATERIALIZED VIEW one TO dst AS SELECT * FROM src"},
		{Database: "db", Name: "two", Definition: "CREATE MATERIALIZED VIEW two TO dst AS SELECT * FROM src"},
	}}
	next, _ := m.Update(mouseAtText(t, m, "two"))
	m = next.(model)
	if m.selected != 1 || !m.overview {
		t.Fatalf("selection failed: %#v", m)
	}
	next, _ = m.Update(mouseAtText(t, m, "two"))
	if next.(model).overview {
		t.Fatal("clicking selected flow did not open its diagram")
	}
}

func TestMouseSelectsLastViewInFirstGroup(t *testing.T) {
	m := model{width: 120, height: 30, overview: true, metricsBusy: true, views: []clickhouse.View{
		{Database: "db", Name: "first_a", Definition: "CREATE MATERIALIZED VIEW first_a TO group_a AS SELECT * FROM src"},
		{Database: "db", Name: "first_b", Definition: "CREATE MATERIALIZED VIEW first_b TO group_b AS SELECT * FROM src"},
		{Database: "db", Name: "last_a", Definition: "CREATE MATERIALIZED VIEW last_a TO group_a AS SELECT * FROM src"},
	}}
	next, _ := m.Update(mouseAtText(t, m, "last_a"))
	if next.(model).selected != 2 || !next.(model).overview {
		t.Fatalf("last view in first group was not selected: %#v", next)
	}
}

func TestMouseWheelScrollsAreaAndHorizontal(t *testing.T) {
	m := model{width: 100, height: 15, definition: true, views: []clickhouse.View{{Name: "mv", Definition: strings.Repeat(strings.Repeat("long line ", 12)+"\n", 30)}, {Name: "second"}}}
	wheel := tea.MouseMsg{X: 80, Y: 6, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	next, _ := m.Update(wheel)
	m = next.(model)
	if m.lineOffset != 3 {
		t.Fatalf("detail offset = %d", m.lineOffset)
	}
	wheel.Shift = true
	next, _ = m.Update(wheel)
	m = next.(model)
	if m.columnOffset == 0 {
		t.Fatal("horizontal scroll did not move")
	}
	wheel.Shift = false
	wheel.X = 2
	next, _ = m.Update(wheel)
	if next.(model).selected != 1 {
		t.Fatal("list wheel did not change selection")
	}
}

func TestMouseOpensColumnsAndLoadsOnSelection(t *testing.T) {
	m := model{width: 120, height: 25, viewRevision: 1, views: []clickhouse.View{{Database: "db", Name: "first"}, {Database: "db", Name: "second"}}}
	next, cmd := m.Update(mouseAtText(t, m, "[c]"))
	m = next.(model)
	if !m.columns || !m.columnsLoading || cmd == nil {
		t.Fatal("columns button did not start loading")
	}
	next, cmd = m.Update(mouseAtText(t, m, "second"))
	m = next.(model)
	if m.selected != 1 || !m.columnsLoading || cmd == nil {
		t.Fatal("mouse selection did not request columns for new view")
	}
}

func TestMouseSelectsTargetColumn(t *testing.T) {
	for _, width := range []int{120, 60} {
		m := model{width: width, height: 30, database: "db", target: true, views: []clickhouse.View{{Name: "mv"}}, targetData: clickhouse.TargetColumns{Table: "out", Columns: []clickhouse.TargetColumn{{Name: "first_col", Type: "UInt64"}, {Name: "second_col", Type: "String"}}}}
		pos := mouseAtText(t, m, "second_col")
		next, _ := m.Update(pos)
		got := next.(model)
		if got.targetColumn != 1 {
			t.Errorf("width %d: target column %d", width, got.targetColumn)
		}
	}
}

func TestOverviewTargetMouseSelectionAndOpen(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO chosen_target AS SELECT * FROM src"}}
	m := model{width: 150, height: 25, database: "db", overview: true, views: views, allViews: views}
	next, _ := m.Update(mouseAtText(t, m, "chosen_target"))
	m = next.(model)
	if !m.overviewTargetFocus || !m.overview {
		t.Fatal("target click did not select target")
	}
	next, cmd := m.Update(mouseAtText(t, m, "chosen_target"))
	m = next.(model)
	if !m.target || !m.targetLoading || cmd == nil {
		t.Fatal("second target click did not open target")
	}
}
