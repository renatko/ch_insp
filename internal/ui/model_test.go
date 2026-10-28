package ui

import (
	"strings"
	"testing"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestDiagramConnectsMultipleSources(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM first JOIN second ON 1"}
	lines := diagramLines(view, clickhouse.Rates{}, false)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"first", "second", "mv", "dst", "──┐", "──┴─▶", "────▶"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diagram missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "db.") {
		t.Fatalf("diagram contains database prefix: %s", joined)
	}
}

func TestDatabaseSelectionFiltersViews(t *testing.T) {
	m := model{databases: []string{"one", "two", "empty"}, database: "one", allViews: []clickhouse.View{{Database: "one", Name: "first"}, {Database: "two", Name: "second"}}, metricsBusy: true}
	m.filterViews()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := next.(model)
	if selected.database != "two" || len(selected.views) != 1 || selected.views[0].Name != "second" {
		t.Fatalf("unexpected selection: %#v", selected)
	}
	next, _ = selected.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected = next.(model)
	if selected.database != "empty" || len(selected.views) != 0 {
		t.Fatalf("empty database selection: %#v", selected)
	}
}

func TestDetailHorizontalScrollShowsTarget(t *testing.T) {
	m := model{views: []clickhouse.View{{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO destination AS SELECT * FROM source"}}}
	m.columnOffset = 40
	content := m.renderDetail(30, 20)
	if !strings.Contains(content, "destination") {
		t.Fatalf("target missing after horizontal scroll: %s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		if lipgloss.Width(line) > 30 {
			t.Fatalf("line exceeds viewport: %q", line)
		}
	}
}

func TestDiagramDisplaysMeasuredRatesAndMissingData(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM source"}
	rates := clickhouse.Rates{Tables: map[string]float64{"db.source": 12.5}, MVAvailable: true, MVRead: 10, MVWritten: 4}
	joined := strings.Join(diagramLines(view, rates, true), "\n")
	for _, want := range []string{"parts: 12.5 rows/s", "read 10.0 / write 4.0 rows/s", "MV: 4.0 rows/s", "total: no data"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diagram missing %q:\n%s", want, joined)
		}
	}
}

func TestTargetShowsIndependentRates(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM source"}
	rates := clickhouse.Rates{Tables: map[string]float64{"db.dst": 25}, MVAvailable: true, MVWritten: 4}
	joined := strings.Join(diagramLines(view, rates, true), "\n")
	if !strings.Contains(joined, "MV: 4.0 rows/s · total: 25.0 rows/s") {
		t.Fatalf("target rates missing: %s", joined)
	}
	rates.MVAvailable = false
	joined = strings.Join(diagramLines(view, rates, true), "\n")
	if !strings.Contains(joined, "MV: no data · total: 25.0 rows/s") {
		t.Fatalf("target rates should be independent: %s", joined)
	}
}

func TestDiagramMetricsBelowBoxes(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM source"}
	lines := diagramLines(view, clickhouse.Rates{}, true)
	if !strings.Contains(lines[3], "╰") || strings.Contains(lines[3], "no data") {
		t.Fatalf("expected box bottoms before metrics: %q", lines[3])
	}
	for _, metric := range []string{"parts: no data", "read/write: no data"} {
		if !strings.Contains(lines[4], metric) {
			t.Errorf("metric %q not below box: %q", metric, lines[4])
		}
	}
}

func TestOverviewShowsFlowsAndRates(t *testing.T) {
	m := model{overviewReady: true, views: []clickhouse.View{
		{Database: "db", Name: "mv1", Definition: "CREATE MATERIALIZED VIEW mv1 TO middle AS SELECT * FROM source JOIN lookup ON 1"},
		{Database: "db", Name: "mv2", Definition: "CREATE MATERIALIZED VIEW mv2 TO result AS SELECT * FROM middle"},
	}, overviewRates: clickhouse.OverviewRates{Tables: map[string]float64{"db.source": 10, "db.middle": 7}, Views: map[string]clickhouse.ViewRate{"db.mv1": {Read: 10, Written: 5}}}}
	lines, starts := m.overviewLines()
	joined := strings.Join(lines, "\n")
	for _, value := range []string{"source", "lookup", "mv1", "middle", "mv2", "result", "parts: 10.0 rows/s", "read 10.0 / write 5.0 rows/s", "total: 7.0 rows/s"} {
		if !strings.Contains(joined, value) {
			t.Errorf("overview missing %q:\n%s", value, joined)
		}
	}
	if len(starts) != 2 || starts[1]-starts[0] != 3 {
		t.Fatalf("unexpected view positions: %v", starts)
	}
	for _, value := range []string{"─▶", "────▶"} {
		if !strings.Contains(joined, value) {
			t.Errorf("overview diagram missing %q", value)
		}
	}
}

func TestOverviewOpenSelectedView(t *testing.T) {
	m := model{overview: true, metricsBusy: true, views: []clickhouse.View{{Name: "one"}, {Name: "two"}}, height: 12}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if next.(model).selected != 1 {
		t.Fatal("selection did not move")
	}
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(model).overview || next.(model).selected != 1 {
		t.Fatal("selected view did not open")
	}
}

func TestOverviewSharesTargetAndShowsTotalRate(t *testing.T) {
	m := model{overviewReady: true, selected: 1, views: []clickhouse.View{
		{Database: "db", Name: "first_mv", Definition: "CREATE MATERIALIZED VIEW first_mv TO destination AS SELECT * FROM first"},
		{Database: "db", Name: "second_mv", Definition: "CREATE MATERIALIZED VIEW second_mv TO destination AS SELECT * FROM second"},
	}, overviewRates: clickhouse.OverviewRates{Tables: map[string]float64{"db.destination": 9}, Views: map[string]clickhouse.ViewRate{"db.first_mv": {Written: 1}, "db.second_mv": {Written: 2}}}}
	lines, starts := m.overviewLines()
	joined := strings.Join(lines, "\n")
	if strings.Count(joined, "destination") != 1 {
		t.Fatalf("expected one target box:\n%s", joined)
	}
	for _, want := range []string{"first_mv", "second_mv", "▶", "total: 9.0 rows/s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "MV:") {
		t.Fatalf("overview target should not show selected MV rate:\n%s", joined)
	}
	if starts[0] >= starts[1] {
		t.Fatalf("unexpected selection positions: %v", starts)
	}
}

func TestOverviewRepeatsSourceAcrossViewsAndTargets(t *testing.T) {
	m := model{overviewReady: true, views: []clickhouse.View{
		{Database: "db", Name: "one", Definition: "CREATE MATERIALIZED VIEW one TO first AS SELECT * FROM shared"},
		{Database: "db", Name: "two", Definition: "CREATE MATERIALIZED VIEW two TO second AS SELECT * FROM shared"},
	}, overviewRates: clickhouse.OverviewRates{Tables: map[string]float64{"db.shared": 12}}}
	lines, _ := m.overviewLines()
	joined := strings.Join(lines, "\n")
	if strings.Count(joined, "shared") != 2 {
		t.Fatalf("source should appear once per view:\n%s", joined)
	}
	if strings.Count(joined, "▶") != 4 {
		t.Fatalf("expected arrows into both views and targets:\n%s", joined)
	}
	for _, want := range []string{"one", "two", "first", "second", "parts: 12.0 rows/s", "▶"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}
}

func TestOverviewKeepsDistinctInternalTargets(t *testing.T) {
	m := model{views: []clickhouse.View{
		{Database: "db", Name: "a", Definition: "CREATE MATERIALIZED VIEW a ENGINE = MergeTree ORDER BY x AS SELECT * FROM src"},
		{Database: "db", Name: "b", Definition: "CREATE MATERIALIZED VIEW b ENGINE = MergeTree ORDER BY x AS SELECT * FROM src"},
	}}
	lines, _ := m.overviewLines()
	if strings.Count(strings.Join(lines, "\n"), "internal storage (no TO)") != 2 {
		t.Fatal("internal targets must remain separate")
	}
}

func TestOverviewColorsAreSharedAndStableAcrossFiltering(t *testing.T) {
	views := []clickhouse.View{
		{Database: "db", Name: "one", Definition: "CREATE MATERIALIZED VIEW one TO dst AS SELECT * FROM a"},
		{Database: "db", Name: "two", Definition: "CREATE MATERIALIZED VIEW two TO dst AS SELECT * FROM b"},
		{Database: "db", Name: "three", Definition: "CREATE MATERIALIZED VIEW three TO other AS SELECT * FROM a"},
		{Database: "db", Name: "internal", Definition: "CREATE MATERIALIZED VIEW internal ENGINE = MergeTree ORDER BY x AS SELECT * FROM a"},
	}
	colors := overviewColors(views, "db")
	first := overviewGroupKey(views[0], clickhouse.InferFlow(views[0]).Target)
	second := overviewGroupKey(views[1], clickhouse.InferFlow(views[1]).Target)
	third := overviewGroupKey(views[2], clickhouse.InferFlow(views[2]).Target)
	if first != second || colors[first] == colors[third] {
		t.Fatalf("unexpected group colors: %#v", colors)
	}
	m := model{database: "db", allViews: views, views: views[:1]}
	before := overviewColors(m.allViews, m.database)[first]
	m.views = views[1:3]
	if after := overviewColors(m.allViews, m.database)[first]; before != after {
		t.Fatal("filtering changed group color")
	}
	if len(colors) != 3 {
		t.Fatalf("expected distinct group for internal storage: %#v", colors)
	}
}

func TestOverviewAlignsRatesUnderNodes(t *testing.T) {
	m := model{database: "db", overviewReady: true, views: []clickhouse.View{
		{Database: "db", Name: "view_one", Definition: "CREATE MATERIALIZED VIEW view_one TO destination AS SELECT * FROM src JOIN users ON 1"},
		{Database: "db", Name: "view_two", Definition: "CREATE MATERIALIZED VIEW view_two TO destination AS SELECT * FROM src"},
	}, overviewRates: clickhouse.OverviewRates{Tables: map[string]float64{"db.src": 12, "db.users": 3, "db.destination": 6}, Views: map[string]clickhouse.ViewRate{"db.view_one": {Read: 9, Written: 5}}}}
	lines, starts := m.overviewLines()
	nameLine, statsLine := ansi.Strip(lines[starts[0]]), ansi.Strip(lines[starts[0]+1])
	col := func(line, value string) int {
		i := strings.Index(line, value)
		if i < 0 {
			t.Fatalf("missing %q in %q", value, line)
		}
		return lipgloss.Width(line[:i])
	}
	if col(nameLine, "src") != col(statsLine, "parts: 12.0 rows/s") || col(nameLine, "users") != col(statsLine, "parts: 3.0 rows/s") || col(nameLine, "view_one") != col(statsLine, "read 9.0 / write 5.0 rows/s") {
		t.Fatalf("statistics are not below their nodes:\n%s\n%s", nameLine, statsLine)
	}
	nameLine, statsLine = ansi.Strip(lines[starts[1]]), ansi.Strip(lines[starts[1]+1])
	if col(nameLine, "destination") != col(statsLine, "total: 6.0 rows/s") {
		t.Fatalf("target statistic is not aligned:\n%s\n%s", nameLine, statsLine)
	}
}

func TestOverviewHighlightsSourceAndMVOnly(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(oldProfile)
	m := model{database: "db", width: 80, height: 30, overview: true, selected: 1, overviewColumn: 8, views: []clickhouse.View{
		{Database: "db", Name: "one", Definition: "CREATE MATERIALIZED VIEW one TO dst AS SELECT * FROM src"},
		{Database: "db", Name: "two", Definition: "CREATE MATERIALIZED VIEW two TO dst AS SELECT * FROM src"},
	}}
	_, positions := m.overviewLines()
	lines := strings.Split(m.renderOverview(m.width), "\n")
	for _, i := range []int{positions[1], positions[1] + 1} {
		if !strings.Contains(lines[i], "48;2;51;65;85") {
			t.Fatalf("line %d is not highlighted: %q", i, lines[i])
		}
	}
	nameLine := lines[positions[1]]
	branch := strings.Index(nameLine, "──┴─▶")
	if branch < 0 || strings.Contains(nameLine[branch:], "48;2;51;65;85") {
		t.Fatalf("target branch is highlighted: %q", nameLine)
	}
	stats := lines[positions[1]+1]
	total := strings.Index(stats, "total:")
	if total < 0 || strings.Contains(stats[total:], "48;2;51;65;85") {
		t.Fatalf("target statistics are highlighted: %q", stats)
	}
	if strings.Contains(lines[positions[0]], "48;2;51;65;85") {
		t.Fatal("unselected row is highlighted")
	}
}
