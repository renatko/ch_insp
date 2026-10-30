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

func TestSearchCanClearFilters(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "a", Definition: "CREATE MATERIALIZED VIEW a TO dst AS SELECT * FROM src"}, {Database: "db", Name: "b", Definition: "CREATE MATERIALIZED VIEW b TO other AS SELECT * FROM hidden"}}
	m := model{database: "db", overview: true, searching: true, allViews: views, filterSource: []string{"db.src"}, searchQuery: "hidden", overviewBusy: true}
	m.filterViews()
	m.searching = true
	if !strings.Contains(m.renderSearch(100), "clear filters") || len(m.searchResults()) != 0 {
		t.Fatal("filtered search did not explain empty result")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	if !m.searching || len(m.filterSource) != 0 || len(m.searchResults()) != 1 || m.searchQuery != "hidden" {
		t.Fatal("clearing filters did not preserve the search")
	}
}

func TestSearchRevealsAdditionalSource(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "joined", Definition: "CREATE MATERIALIZED VIEW joined TO dst AS SELECT * FROM events JOIN users ON 1 JOIN regions ON 1"}}
	m := model{width: 80, height: 10, database: "db", overview: true, searching: true, searchQuery: "regions", views: views, allViews: views, overviewReady: true}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.searching || !m.overview || !strings.Contains(m.View(), "regions [I]") {
		t.Fatalf("additional source is not visible after search:\n%s", m.View())
	}
}
