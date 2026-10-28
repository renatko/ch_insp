package ui

import (
	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestSearchFindsNodesAndNavigates(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "first_mv", Definition: "CREATE MATERIALIZED VIEW first_mv TO shared_target AS SELECT * FROM events"}, {Database: "db", Name: "second_mv", Definition: "CREATE MATERIALIZED VIEW second_mv TO shared_target AS SELECT * FROM events"}}
	m := model{width: 80, height: 20, database: "db", overview: true, views: views, allViews: views}
	next, _ := m.Update(mouseKey("/"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	m = next.(model)
	if len(m.searchResults()) != 1 || m.searchResults()[0].kind != "target" {
		t.Fatalf("target repeated: %#v", m.searchResults())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.searching || !m.overviewTargetFocus {
		t.Fatal("target search did not navigate")
	}
	next, _ = m.Update(mouseKey("/"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("events")})
	m = next.(model)
	if len(m.searchResults()) != 2 || !strings.Contains(m.renderSearch(80), "second_mv") {
		t.Fatal("source occurrences not shown")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.selected != 1 || m.overviewTargetFocus {
		t.Fatal("source search navigated to wrong flow")
	}
}
