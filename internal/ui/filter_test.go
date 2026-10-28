package ui

import (
	"testing"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
)

func filterFixture() model {
	return model{width: 120, height: 30, database: "db", metricsBusy: true, overviewBusy: true, allViews: []clickhouse.View{
		{Database: "db", Name: "one", Definition: "CREATE MATERIALIZED VIEW one TO db.out AS SELECT * FROM db.events JOIN db.users ON 1"},
		{Database: "db", Name: "two", Definition: "CREATE MATERIALIZED VIEW two TO db.other AS SELECT * FROM db.events"},
		{Database: "other", Name: "three", Definition: "CREATE MATERIALIZED VIEW three TO db.out AS SELECT * FROM db.users"},
	}}
}

func TestFilterSourceTargetAndDatabase(t *testing.T) {
	m := filterFixture()
	m.filterSource, m.filterTarget = "USERS", "out"
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "one" {
		t.Fatalf("unexpected results: %#v", m.views)
	}
	m.database = "other"
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "three" {
		t.Fatalf("filter did not follow database: %#v", m.views)
	}
	m.filterTarget = "nonexistent"
	m.filterViews()
	if len(m.views) != 0 {
		t.Fatal("expected no matches")
	}
}

func TestFilterFormApplyCancelAndClear(t *testing.T) {
	m := filterFixture()
	m.filterViews()
	next, _ := m.Update(mouseKey("f"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("users")})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("out")})
	m = next.(model)
	next, _ = m.Update(mouseKey("enter"))
	m = next.(model)
	if m.filtering || len(m.views) != 1 || m.views[0].Name != "one" {
		t.Fatalf("apply failed: %#v", m)
	}
	next, _ = m.Update(mouseKey("f"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(model)
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if m.filterSource != "users" {
		t.Fatal("cancel changed active filter")
	}
	next, _ = m.Update(mouseKey("f"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	if m.filterSource != "" || m.filterTarget != "" || len(m.views) != 2 {
		t.Fatal("clear failed")
	}
}

func TestFilterMouseAndStaleMetrics(t *testing.T) {
	m := filterFixture()
	m.filterViews()
	m.overview = true
	next, _ := m.Update(mouseAtText(t, m, "[f]"))
	m = next.(model)
	next, _ = m.Update(mouseAtText(t, m, "TARGET"))
	m = next.(model)
	if m.filterField != 1 {
		t.Fatal("click did not focus target")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("out")})
	m = next.(model)
	oldRevision := m.viewRevision
	next, _ = m.Update(mouseAtText(t, m, "[enter]"))
	m = next.(model)
	if m.filterTarget != "out" || len(m.views) != 1 {
		t.Fatal("click did not apply filter")
	}
	next, _ = m.Update(overviewLoadedMsg{database: "db", revision: oldRevision})
	m = next.(model)
	if m.overviewReady {
		t.Fatal("accepted stale overview metrics")
	}
}

func TestUnknownTargetDoesNotMatchTableName(t *testing.T) {
	view := clickhouse.View{Database: "db", Definition: "CREATE MATERIALIZED VIEW mv ENGINE = MergeTree ORDER BY x AS SELECT * FROM src"}
	if matchesFlow(view, "", "internal") {
		t.Fatal("internal storage is not a table name")
	}
}
