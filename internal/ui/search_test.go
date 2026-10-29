package ui

import (
	"ch_insp/internal/clickhouse"
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

func TestSearchAcceptsSpacesInQuotedNames(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "order history", Definition: "CREATE MATERIALIZED VIEW `order history` TO target AS SELECT * FROM source"}
	m := model{searching: true, views: []clickhouse.View{view}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("order")})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("history")})
	m = next.(model)
	if m.searchQuery != "order history" || len(m.searchResults()) != 1 {
		t.Fatalf("search failed: %q %#v", m.searchQuery, m.searchResults())
	}
}

func TestSearchJumpClearsPreviousDetailMode(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM src"}}
	m := model{width: 80, height: 20, database: "db", views: views, allViews: views, columns: true}
	if m.loadColumns() == nil {
		t.Fatal("column request not started")
	}
	next, _ := m.Update(mouseKey("/"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("src")})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.overview || m.columns || m.columnsLoading || m.columnsCancel != nil || cmd == nil {
		t.Fatal("search retained old detail mode or did not load overview")
	}
}

func TestSearchRespectsVisibleViewsAndDistinctTargets(t *testing.T) {
	all := []clickhouse.View{
		{Database: "one", Name: "mv_a", Definition: "CREATE MATERIALIZED VIEW mv_a TO one.out AS SELECT * FROM one.events"},
		{Database: "one", Name: "mv_b", Definition: "CREATE MATERIALIZED VIEW mv_b TO two.out AS SELECT * FROM two.events"},
		{Database: "two", Name: "hidden", Definition: "CREATE MATERIALIZED VIEW hidden TO two.hidden_out AS SELECT * FROM two.events"},
	}
	m := model{database: "one", allViews: all, views: all[:2], searchQuery: "out"}
	results := m.searchResults()
	if len(results) != 2 || results[0].name != "one.out" || results[1].name != "two.out" {
		t.Fatalf("wrong visible results: %#v", results)
	}
	m.searchQuery = "events"
	results = m.searchResults()
	if len(results) != 2 || results[0].name != "one.events" || results[1].name != "two.events" {
		t.Fatalf("wrong source results: %#v", results)
	}
	m.searchQuery = "hidden"
	if len(m.searchResults()) != 0 {
		t.Fatal("search exposed a filtered view")
	}
}
