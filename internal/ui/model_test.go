package ui

import (
	"strings"
	"testing"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	for _, value := range []string{"source", "lookup", "mv1", "middle", "mv2", "result", "parts: 10.0 rows/s", "read 10.0 / write 5.0 rows/s", "MV: 5.0 rows/s · total: 7.0 rows/s"} {
		if !strings.Contains(joined, value) {
			t.Errorf("overview missing %q:\n%s", value, joined)
		}
	}
	if len(starts) != 2 || starts[1]-starts[0] != 12 {
		t.Fatalf("unexpected view positions: %v", starts)
	}
	for _, value := range []string{"╭", "╰", "──┐", "──┴─▶"} {
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
