package ui

import (
	"fmt"
	"strings"
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
	m.filterSource, m.filterTarget = "db.users", "db.out"
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "one" {
		t.Fatalf("unexpected results: %#v", m.views)
	}
	m.database = "other"
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "three" {
		t.Fatalf("filter did not follow database: %#v", m.views)
	}
	m.filterTarget = "db.nonexistent"
	m.filterViews()
	if len(m.views) != 0 {
		t.Fatal("expected no matches")
	}
	if matchesFlow(m.allViews[0], "db.user", "") {
		t.Fatal("filter should match the selected table exactly")
	}
}

func TestFilterListApplyCancelAndClear(t *testing.T) {
	m := filterFixture()
	m.filterViews()
	next, _ := m.Update(mouseKey("f"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(mouseKey("enter"))
	m = next.(model)
	if m.filtering || m.filterSource != "db.users" || m.filterTarget != "db.out" || len(m.views) != 1 || m.views[0].Name != "one" {
		t.Fatalf("apply failed: %#v", m)
	}
	next, _ = m.Update(mouseKey("f"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(model)
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if m.filterSource != "db.users" {
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
	next, _ = m.Update(mouseAtText(t, m, "[target]"))
	m = next.(model)
	if m.filterField != 1 {
		t.Fatal("click did not focus target")
	}
	next, _ = m.Update(mouseAtText(t, m, "out"))
	m = next.(model)
	if m.draftTarget != "db.out" {
		t.Fatal("click did not select target")
	}
	oldRevision := m.viewRevision
	next, _ = m.Update(mouseAtText(t, m, "[enter]"))
	m = next.(model)
	if m.filterTarget != "db.out" || len(m.views) != 1 {
		t.Fatal("click did not apply filter")
	}
	next, _ = m.Update(overviewLoadedMsg{database: "db", revision: oldRevision})
	m = next.(model)
	if m.overviewReady {
		t.Fatal("accepted stale overview metrics")
	}
}

func TestFilterListsDisambiguateAndKeepMissingSelection(t *testing.T) {
	m := filterFixture()
	m.allViews = append(m.allViews, clickhouse.View{Database: "db", Name: "four", Definition: "CREATE MATERIALIZED VIEW four TO db.out AS SELECT * FROM other.events"})
	m.filterSource = "removed.events"
	m.openFilter()
	text := m.renderFilter(m.width)
	for _, want := range []string{"events  [database: db]", "events  [database: other]", "events  [database: removed]  (not in current database)"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	m.selectFilterOption(0, 0)
	if m.draftSource != "" || m.filterCursors[0] != 0 {
		t.Fatal("could not clear missing selection")
	}
}

func TestFilterWheelScrollAndChoose(t *testing.T) {
	m := filterFixture()
	m.height = 15
	for i := 0; i < 20; i++ {
		m.allViews = append(m.allViews, clickhouse.View{Database: "db", Name: fmt.Sprintf("v%d", i), Definition: fmt.Sprintf("CREATE MATERIALIZED VIEW v%d TO db.out AS SELECT * FROM db.src%02d", i, i)})
	}
	m.openFilter()
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, X: 4, Y: 8})
	m = next.(model)
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, X: 4, Y: 8})
	m = next.(model)
	if m.filterCursors[0] != 6 || m.filterStart() == 0 {
		t.Fatalf("wheel did not scroll: cursor %d start %d", m.filterCursors[0], m.filterStart())
	}
	clicked := m.filterOptions(0)[m.filterStart()].value
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 3, Y: 7})
	m = next.(model)
	if m.draftSource != clicked {
		t.Fatalf("selected %q; expected %q", m.draftSource, clicked)
	}
}

func TestUnknownTargetDoesNotMatchTableName(t *testing.T) {
	view := clickhouse.View{Database: "db", Definition: "CREATE MATERIALIZED VIEW mv ENGINE = MergeTree ORDER BY x AS SELECT * FROM src"}
	if matchesFlow(view, "", "internal") {
		t.Fatal("internal storage is not a table name")
	}
}
