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
