package ui

import (
	"fmt"
	"regexp"
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
	for _, want := range []string{"inserted: 12.5 rows/s", "read 10.0 rows/s / write 4.0 rows/s", "MV: 4.0 rows/s", "total: no data"} {
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
	for _, metric := range []string{"inserted: no data", "read/write: no data"} {
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
	for _, value := range []string{"source", "lookup", "mv1", "middle", "mv2", "result", "inserted: 10.0 rows/s", "read 10.0 rows/s / write 5.0 rows/s", "total: 7.0 rows/s"} {
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

func TestOverviewKeyboardFollowsGroupedVisualOrder(t *testing.T) {
	m := model{database: "db", width: 120, height: 12, overview: true, views: []clickhouse.View{
		{Database: "db", Name: "a_first", Definition: "CREATE MATERIALIZED VIEW a_first TO group_a AS SELECT * FROM source"},
		{Database: "db", Name: "b_first", Definition: "CREATE MATERIALIZED VIEW b_first TO group_b AS SELECT * FROM source"},
		{Database: "db", Name: "a_last", Definition: "CREATE MATERIALIZED VIEW a_last TO group_a AS SELECT * FROM source"},
		{Database: "db", Name: "b_last", Definition: "CREATE MATERIALIZED VIEW b_last TO group_b AS SELECT * FROM source"},
	}}
	for _, expected := range []string{"a_last", "b_first", "b_last", "b_last"} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
		if m.views[m.selected].Name != expected {
			t.Fatalf("down selected %s, expected %s", m.views[m.selected].Name, expected)
		}
		lines, positions := m.overviewLines()
		start := minInt(m.overviewScroll, maxInt(0, len(lines)-maxInt(1, m.height-7)))
		if positions[m.selected] < start || positions[m.selected]+1 >= start+maxInt(1, m.height-7) {
			t.Fatalf("selected item is not visible: scroll=%d position=%d", start, positions[m.selected])
		}
	}
	for _, expected := range []string{"b_first", "a_last", "a_first", "a_first"} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(model)
		if m.views[m.selected].Name != expected {
			t.Fatalf("up selected %s, expected %s", m.views[m.selected].Name, expected)
		}
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
	for _, want := range []string{"one", "two", "first", "second", "inserted: 12.0 rows/s", "▶"} {
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
	if col(nameLine, "src") != col(statsLine, "inserted: 12.0 rows/s") || col(nameLine, "users") != col(statsLine, "inserted: 3.0 rows/s") || col(nameLine, "view_one") != col(statsLine, "read 9.0 rows/s / write 5.0 rows/s") {
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
	background := regexp.MustCompile("\x1b\\[[0-9;]*48;2;51;65;85m")
	backgroundEnd := func(line string) int {
		matches := background.FindAllStringIndex(line, -1)
		if len(matches) == 0 {
			t.Fatalf("missing background in %q", line)
		}
		end := matches[len(matches)-1][1]
		reset := strings.Index(line[end:], "\x1b[0m")
		if reset < 0 {
			t.Fatalf("missing reset in %q", line)
		}
		return lipgloss.Width(line[:end+reset])
	}
	if first, second := backgroundEnd(lines[positions[1]]), backgroundEnd(lines[positions[1]+1]); first != second {
		t.Fatalf("highlight ends in different columns: flow %d, stats %d", first, second)
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

func TestColumnDetailsNavigationAndStaleResults(t *testing.T) {
	m := model{width: 120, height: 30, viewRevision: 5, views: []clickhouse.View{{Database: "db", Name: "first", Definition: "CREATE MATERIALIZED VIEW first TO out AS SELECT id FROM src"}, {Database: "db", Name: "second", Definition: "CREATE MATERIALIZED VIEW second TO out AS SELECT n FROM src"}}}
	next, cmd := m.Update(mouseKey("c"))
	m = next.(model)
	if !m.columns || !m.columnsLoading || cmd == nil {
		t.Fatal("columns mode did not start loading")
	}
	data := clickhouse.ColumnLineage{Columns: []clickhouse.ColumnMapping{{Output: "id", Expression: "id", Inputs: []string{"src.id (UInt64)"}, ViewType: "UInt64", TargetType: "UInt64", TargetStatus: "matched by name"}}, Joins: []string{"LEFT JOIN users ON src.id = users.id"}, Where: "WHERE src.id > 0", GroupBy: "GROUP BY src.id", AdditionalReads: []string{"src.key (String)"}, TargetExtras: []string{"extra (String) DEFAULT 'x'"}}
	next, _ = m.Update(columnsLoadedMsg{key: "db\x00first", revision: 5, request: m.columnsRequest, data: data})
	m = next.(model)
	if m.columnsLoading {
		t.Fatal("loading not cleared")
	}
	content := strings.Join(m.detailLines(), "\n")
	for _, part := range []string{"COLUMN FLOW", "QUERY CONDITIONS", "LEFT JOIN users ON src.id = users.id", "WHERE src.id > 0", "GROUP BY src.id", "OUTPUT  id", "SQL:   id", "src.id (UInt64)", "target: id (UInt64)", "src.key (String)", "extra (String) DEFAULT 'x'"} {
		if !strings.Contains(content, part) {
			t.Errorf("missing %q in %s", part, content)
		}
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	if m.selected != 1 || !m.columnsLoading || cmd == nil {
		t.Fatal("new view metadata did not load")
	}
	next, _ = m.Update(columnsLoadedMsg{key: "db\x00first", revision: 5, request: m.columnsRequest - 1, data: data})
	m = next.(model)
	if !m.columnsLoading {
		t.Fatal("stale result replaced current selection")
	}
	next, _ = m.Update(columnsLoadedMsg{key: "db\x00second", revision: 4, request: m.columnsRequest, data: data})
	m = next.(model)
	if !m.columnsLoading {
		t.Fatal("old catalog revision accepted")
	}
	next, _ = m.Update(mouseKey("d"))
	m = next.(model)
	if m.columns || !m.definition {
		t.Fatal("SQL view did not open")
	}
}

func TestColumnDetailsIgnoreEarlierRequestForSameView(t *testing.T) {
	m := model{columns: true, viewRevision: 2, views: []clickhouse.View{{Database: "db", Name: "mv"}}}
	_ = m.loadColumns()
	oldRequest := m.columnsRequest
	_ = m.loadColumns()
	next, _ := m.Update(columnsLoadedMsg{key: "db\x00mv", revision: 2, request: oldRequest})
	m = next.(model)
	if !m.columnsLoading {
		t.Fatal("earlier request replaced a newer request")
	}
	m.cancelColumns()
}

func TestTargetScreenNavigationAndStaleResult(t *testing.T) {
	m := model{width: 120, height: 30, database: "db", overview: true, viewRevision: 3, views: []clickhouse.View{{Database: "db", Name: "first"}, {Database: "db", Name: "second"}}}
	next, cmd := m.Update(mouseKey("t"))
	m = next.(model)
	if !m.target || m.overview || !m.targetLoading || cmd == nil {
		t.Fatal("target screen did not load from overview")
	}
	data := clickhouse.TargetColumns{Database: "db", Table: "out", Views: []string{"db.first", "other.second"}, Columns: []clickhouse.TargetColumn{{Name: "id", Type: "UInt64", Writers: []clickhouse.TargetWriter{{View: "db.first", Expression: "e.id", Inputs: []string{"events.id (UInt64)"}}, {View: "other.second", Expression: "o.id"}}}, {Name: "extra", Type: "String", DefaultKind: "DEFAULT", DefaultExpression: "'x'"}}}
	next, _ = m.Update(targetLoadedMsg{key: "db\x00first", revision: 3, request: m.columnsRequest, data: data})
	m = next.(model)
	content := strings.Join(m.detailLines(), "\n")
	for _, v := range []string{"TARGET TABLE", "out", "id (UInt64)", "first", "other.second", "events.id (UInt64)", "no MV", "direct inserts"} {
		if !strings.Contains(content, v) {
			t.Errorf("missing %q: %s", v, content)
		}
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	if m.selected != 0 || m.targetColumn != 1 || cmd != nil {
		t.Fatal("column selection did not move")
	}
	content = strings.Join(m.detailLines(), "\n")
	if !strings.Contains(content, "DEFAULT 'x'") || !strings.Contains(content, "No identified MV output") {
		t.Fatalf("missing selected column details: %s", content)
	}
	next, cmd = m.Update(mouseAtText(t, m, "second"))
	m = next.(model)
	if m.selected != 1 || !m.targetLoading || cmd == nil {
		t.Fatal("new target was not loaded")
	}
	next, _ = m.Update(targetLoadedMsg{key: "db\x00first", revision: 3, request: m.columnsRequest - 1, data: data})
	m = next.(model)
	if !m.targetLoading {
		t.Fatal("stale target result was accepted")
	}
	next, _ = m.Update(mouseKey("t"))
	m = next.(model)
	if m.target || !m.overview {
		t.Fatal("did not return to overview")
	}
}

func TestTargetColumnWindowAndNarrowWriterCount(t *testing.T) {
	cols := make([]clickhouse.TargetColumn, 20)
	for i := range cols {
		cols[i] = clickhouse.TargetColumn{Name: fmt.Sprintf("col_%02d", i), Type: "UInt64", Writers: []clickhouse.TargetWriter{{View: "db.extremely_long_materialized_view_one"}, {View: "db.extremely_long_materialized_view_two"}}}
	}
	m := model{width: 60, height: 22, database: "db", target: true, views: []clickhouse.View{{Name: "mv"}}, targetData: clickhouse.TargetColumns{Table: "target", Columns: cols}}
	for i := 0; i < 19; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
	}
	start, end := m.targetColumnWindow()
	if m.targetColumn != 19 || start == 0 || end != 20 {
		t.Fatalf("selection/window: %d %d:%d", m.targetColumn, start, end)
	}
	content := strings.Join(m.targetLines(), "\n")
	if !strings.Contains(content, "col_19") || !strings.Contains(content, "← 2 MV") || strings.Contains(content, "col_00") {
		t.Fatalf("unexpected compact list: %s", content)
	}
}

func TestTargetTypesCompactAndShowDifferences(t *testing.T) {
	targetType := "Nullable(LowCardinality(String))"
	m := model{width: 100, height: 30, database: "db", target: true, views: []clickhouse.View{{Name: "mv"}}, targetData: clickhouse.TargetColumns{Columns: []clickhouse.TargetColumn{{Name: "text", Type: targetType, Writers: []clickhouse.TargetWriter{{View: "db.diff", ViewType: "String"}, {View: "db.same", ViewType: targetType}, {View: "db.unknown"}}}}}}
	lines := m.targetLines()
	if !strings.Contains(lines[4], "…") || strings.Contains(lines[4], targetType) {
		t.Fatalf("type was not compacted: %s", lines[4])
	}
	content := strings.Join(lines, "\n")
	if !strings.Contains(content, "TARGET TYPE: "+targetType) || !strings.Contains(content, "MV TYPE: String → target: "+targetType) || strings.Count(content, "MV TYPE:") != 1 {
		t.Fatalf("incorrect type details: %s", content)
	}
}

func TestChartsShowFiveIntervals(t *testing.T) {
	hist := clickhouse.ViewHistory{Read: [5]uint64{0, 10, 20, 0, 40}, Written: [5]uint64{5, 5, 0, 5, 5}}
	tables := [5]uint64{1, 2, 3, 4, 5}
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM source"}
	rates := clickhouse.Rates{MVAvailable: true, MVRead: 1, MVWritten: 2, MVHistory: &hist, Tables: map[string]float64{"db.dst": 3}, TableHistory: map[string][5]uint64{"db.dst": tables}}
	detail := strings.Join(diagramLines(view, rates, true), "\n")
	for _, part := range []string{"read ·▂▄·█ 1.0 rows/s", "write ██·██ 2.0 rows/s", "total: ▂▄▅▇█ 3.0 rows/s"} {
		if !strings.Contains(detail, part) {
			t.Errorf("missing %q: %s", part, detail)
		}
	}
	overview := clickhouse.OverviewRates{Views: map[string]clickhouse.ViewRate{"db.mv": {Read: 1, Written: 2}}, History: map[string]clickhouse.ViewHistory{"db.mv": hist}, Tables: rates.Tables, TableHistory: rates.TableHistory}
	lines, _ := overviewGraph([]clickhouse.View{view}, overview, true, 0, map[string]lipgloss.Color{})
	for _, part := range []string{"read ·▂▄·█ 1.0 rows/s", "total: ▂▄▅▇█ 3.0 rows/s"} {
		if !strings.Contains(strings.Join(lines, "\n"), part) {
			t.Errorf("overview missing %q", part)
		}
	}
}

func TestMetricColorsInDetailAndOverview(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(oldProfile)
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM src"}
	rates := clickhouse.Rates{Tables: map[string]float64{"db.src": 4, "db.dst": 7}, MVAvailable: true, MVRead: 3, MVWritten: 2}
	detail := strings.Join(diagramLines(view, rates, true), "\n")
	overview := clickhouse.OverviewRates{Tables: rates.Tables, Views: map[string]clickhouse.ViewRate{"db.mv": {Read: 3, Written: 2}}}
	lines, _ := overviewGraph([]clickhouse.View{view}, overview, true, 0, map[string]lipgloss.Color{})
	for _, text := range []string{detail, strings.Join(lines, "\n")} {
		for _, item := range []struct{ name, color string }{{"inserted:", "38;2;107;169;191"}, {"read ", "38;2;107;169;191"}, {"write ", "38;2;187;162;105"}, {"total:", "38;2;187;162;105"}} {
			pos := strings.Index(text, item.name)
			if pos < 0 {
				t.Errorf("missing %s", item.name)
				continue
			}
			prefix := text[:pos]
			color := strings.LastIndex(prefix, item.color)
			reset := strings.LastIndex(prefix, "\x1b[0m")
			if color < 0 || color < reset {
				t.Errorf("%s not using expected color: %q", item.name, text)
			}
		}
	}
}

func TestOverviewAlignsMetricsAndTargetsAcrossGroups(t *testing.T) {
	views := []clickhouse.View{
		{Database: "db", Name: "short_mv", Definition: "CREATE MATERIALIZED VIEW short_mv TO target_a AS SELECT * FROM long_source_name"},
		{Database: "db", Name: "another_mv", Definition: "CREATE MATERIALIZED VIEW another_mv TO target_a AS SELECT * FROM src"},
		{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO target_b AS SELECT * FROM x"},
	}
	rates := clickhouse.OverviewRates{Tables: map[string]float64{"db.target_a": 1, "db.target_b": 2}, Views: map[string]clickhouse.ViewRate{"db.short_mv": {Read: 2, Written: 5}, "db.another_mv": {Read: 12345, Written: 5}, "db.mv": {Read: 12, Written: 6}}, History: map[string]clickhouse.ViewHistory{"db.short_mv": {Read: [5]uint64{1, 2, 3, 4, 5}}}}
	lines, positions := overviewGraph(views, rates, true, 0, map[string]lipgloss.Color{})
	col := func(line, pattern string) int {
		line = ansi.Strip(line)
		i := strings.Index(line, pattern)
		if i < 0 {
			t.Fatalf("missing %q in %q", pattern, line)
		}
		return lipgloss.Width(line[:i])
	}
	readCol, writeCol := col(lines[positions[0]+1], "read "), col(lines[positions[0]+1], "write ")
	for _, pos := range positions {
		if col(lines[pos+1], "read ") != readCol || col(lines[pos+1], "write ") != writeCol {
			t.Fatalf("unaligned metrics:\n%s", strings.Join(lines, "\n"))
		}
	}
	a, b := positions[1], positions[2]
	if col(lines[a], "target_a") != col(lines[b], "target_b") || col(lines[a+1], "total:") != col(lines[b+1], "total:") {
		t.Fatalf("unaligned targets:\n%s", strings.Join(lines, "\n"))
	}
}

func TestOverviewTargetKeyboardNavigation(t *testing.T) {
	views := []clickhouse.View{
		{Database: "db", Name: "first", Definition: "CREATE MATERIALIZED VIEW first TO dst_a AS SELECT * FROM src"},
		{Database: "db", Name: "second", Definition: "CREATE MATERIALIZED VIEW second TO dst_a AS SELECT * FROM src"},
		{Database: "db", Name: "third", Definition: "CREATE MATERIALIZED VIEW third TO dst_b AS SELECT * FROM src"},
	}
	m := model{width: 35, height: 16, database: "db", overview: true, views: views, allViews: views}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	if !m.overviewTargetFocus || m.overviewColumn == 0 {
		t.Fatalf("target not selected or scrolled into view: %#v", m)
	}
	pos, ok := m.selectedOverviewTarget()
	if !ok || pos.key != "db.dst_a" {
		t.Fatalf("wrong target: %#v", pos)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	pos, ok = m.selectedOverviewTarget()
	if !ok || pos.key != "db.dst_b" {
		t.Fatalf("target navigation failed: %#v", pos)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.target || m.overview || !m.targetLoading || cmd == nil || m.views[m.selected].Name != "third" {
		t.Fatal("selected target did not open")
	}
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if !m.overview || !m.overviewTargetFocus {
		t.Fatal("overview target focus was not restored")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(model)
	if m.overviewTargetFocus {
		t.Fatal("left did not return focus to MV")
	}
}

func TestOverviewTargetSelectionHasEvenWidth(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(oldProfile)
	for _, target := range []string{"dst", "very_long_destination_name_exceeding_statistics_width"} {
		view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO " + target + " AS SELECT * FROM src"}
		rates := clickhouse.OverviewRates{Tables: map[string]float64{"db." + target: 20.1}, TableHistory: map[string][5]uint64{"db." + target: {1, 2, 3, 4, 5}}}
		lines, positions := overviewGraphFocused([]clickhouse.View{view}, rates, true, 0, map[string]lipgloss.Color{}, true)
		bg := regexp.MustCompile("\\x1b\\[[0-9;]*48;2;51;65;85m")
		extent := func(line string) (int, int) {
			matches := bg.FindAllStringIndex(line, -1)
			if len(matches) == 0 {
				t.Fatalf("missing target selection: %q", line)
			}
			first := lipgloss.Width(line[:matches[0][0]])
			last := matches[len(matches)-1][1]
			end := strings.Index(line[last:], "\x1b[0m")
			if end < 0 {
				t.Fatalf("missing reset: %q", line)
			}
			return first, lipgloss.Width(line[:last+end])
		}
		if !strings.Contains(ansi.Strip(lines[positions[0]]), "› "+target) {
			t.Fatalf("target marker missing: %s", lines[positions[0]])
		}
		firstStart, firstEnd := extent(lines[positions[0]])
		secondStart, secondEnd := extent(lines[positions[0]+1])
		if firstStart != secondStart || firstEnd != secondEnd {
			t.Errorf("uneven selection for %s: %d:%d vs %d:%d", target, firstStart, firstEnd, secondStart, secondEnd)
		}
	}
}
