package ui

import (
	"fmt"
	"strings"
	"testing"

	"ch_insp/internal/clickhouse"
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
	m.filterSource, m.filterTarget = []string{"db.users"}, []string{"db.out"}
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "one" {
		t.Fatalf("unexpected results: %#v", m.views)
	}
	m.database = "other"
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "three" {
		t.Fatalf("filter did not follow database: %#v", m.views)
	}
	m.filterTarget = []string{"db.nonexistent"}
	m.filterViews()
	if len(m.views) != 0 {
		t.Fatal("expected no matches")
	}
	if matchesFlow(m.allViews[0], []string{"db.user"}, nil) {
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
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(model)
	next, _ = m.Update(mouseKey("enter"))
	m = next.(model)
	if m.filtering || !contains(m.filterSource, "db.users") || !contains(m.filterTarget, "db.out") || len(m.views) != 1 || m.views[0].Name != "one" {
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
	if !contains(m.filterSource, "db.users") {
		t.Fatal("cancel changed active filter")
	}
	next, _ = m.Update(mouseKey("f"))
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	if len(m.filterSource) != 0 || len(m.filterTarget) != 0 || len(m.views) != 2 {
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
	if !contains(m.draftTarget, "db.out") {
		t.Fatal("click did not select target")
	}
	oldRevision := m.viewRevision
	next, _ = m.Update(mouseAtText(t, m, "[enter]"))
	m = next.(model)
	if !contains(m.filterTarget, "db.out") || len(m.views) != 1 {
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
	m.filterSource = []string{"removed.events"}
	m.openFilter()
	text := m.renderFilter(m.width)
	for _, want := range []string{"events  [database: db]", "events  [database: other]", "events  [database: removed]  (not in current database)"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	m.selectFilterOption(0, 0)
	m.selectFilterOption(0, 0)
	if len(m.draftSource) != 0 || m.filterCursors[0] != 0 {
		t.Fatal("could not clear missing selection")
	}
}

func TestAnyTableSelectsAndClearsAllOptions(t *testing.T) {
	m := filterFixture()
	m.openFilter()
	options := m.filterOptions(0)
	m.selectFilterOption(0, 0)
	if len(m.draftSource) != len(options)-1 || !allFilterOptionsSelected(options, m.draftSource) {
		t.Fatalf("expected every source selected: %v", m.draftSource)
	}
	if !strings.Contains(m.renderFilter(m.width), "[x] (any table)") {
		t.Fatal("select all checkbox is not checked")
	}
	if !strings.Contains(m.renderFilter(m.width), "[x] events") || !strings.Contains(m.renderFilter(m.width), "[x] users") {
		t.Fatal("individual table checkboxes are not checked")
	}
	m.selectFilterOption(0, 1)
	if allFilterOptionsSelected(m.filterOptions(0), m.draftSource) {
		t.Fatal("select all should be unchecked after deselecting an entry")
	}
	m.selectFilterOption(0, 0)
	if !allFilterOptionsSelected(m.filterOptions(0), m.draftSource) {
		t.Fatal("select all did not restore the missing entry")
	}
	m.selectFilterOption(0, 0)
	if len(m.draftSource) != 0 {
		t.Fatal("second click should clear the list")
	}
	m.selectFilterOption(1, 0)
	if len(m.draftTarget) != 2 || len(m.draftSource) != 0 {
		t.Fatal("target select all must not change sources")
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
	if !contains(m.draftSource, clicked) {
		t.Fatalf("selected %v; expected %q", m.draftSource, clicked)
	}
}

func TestMultipleSelectionUsesAnyWithinEachList(t *testing.T) {
	m := filterFixture()
	m.filterSource = []string{"db.events", "db.users"}
	m.filterTarget = []string{"db.out", "db.other"}
	m.filterViews()
	if len(m.views) != 2 {
		t.Fatalf("expected both matching views, got %v", m.views)
	}
	m.filterSource = []string{"db.users"}
	m.filterViews()
	if len(m.views) != 1 || m.views[0].Name != "one" {
		t.Fatalf("expected source AND target matching: %v", m.views)
	}
	m.filterTarget = []string{"db.other"}
	m.filterViews()
	if len(m.views) != 0 {
		t.Fatalf("expected no view matching both filters: %v", m.views)
	}
}

func TestMultipleSelectionToggleAndCancel(t *testing.T) {
	m := filterFixture()
	m.filterSource = []string{"db.events"}
	m.openFilter()
	options := m.filterOptions(0)
	var users, events int
	for i, option := range options {
		if option.value == "db.users" {
			users = i
		}
		if option.value == "db.events" {
			events = i
		}
	}
	m.selectFilterOption(0, users)
	if !contains(m.draftSource, "db.events") || !contains(m.draftSource, "db.users") {
		t.Fatalf("failed to select both: %v", m.draftSource)
	}
	m.selectFilterOption(0, events)
	if contains(m.draftSource, "db.events") || !contains(m.draftSource, "db.users") {
		t.Fatalf("failed to toggle one: %v", m.draftSource)
	}
	next, _ := m.Update(mouseKey("esc"))
	m = next.(model)
	if len(m.filterSource) != 1 || m.filterSource[0] != "db.events" {
		t.Fatalf("cancel changed active selection: %v", m.filterSource)
	}
}

func TestUnknownTargetDoesNotMatchTableName(t *testing.T) {
	view := clickhouse.View{Database: "db", Definition: "CREATE MATERIALIZED VIEW mv ENGINE = MergeTree ORDER BY x AS SELECT * FROM src"}
	if matchesFlow(view, nil, []string{"internal"}) {
		t.Fatal("internal storage is not a table name")
	}
}
