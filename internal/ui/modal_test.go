package ui

import (
	"strings"
	"testing"

	"ch_insp/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestModalScreensFitAndKeepOverview(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "sample_mv", Definition: "CREATE MATERIALIZED VIEW sample_mv TO dst AS SELECT * FROM src"}}
	for _, tc := range []struct {
		name, title string
		set         func(*model)
	}{
		{"diagram", "DATA FLOW", func(m *model) { m.overview = false }},
		{"sql", "CREATE DEFINITION", func(m *model) { m.overview = false; m.definition = true }},
		{"columns", "COLUMN FLOW", func(m *model) { m.overview = false; m.columns = true }},
		{"target", "TARGET TABLE", func(m *model) { m.overview = false; m.target = true }},
		{"dependencies", "TABLE DEPENDENCIES", func(m *model) { m.overview = false; m.dependencies = true; m.dependencyTable = "db.src" }},
		{"errors", "MV ERRORS", func(m *model) { m.overview = false; m.failuresMode = true }},
		{"nodes", "SELECT NODE", func(m *model) { m.choosingNode = true }},
		{"database", "SELECT DATABASE", func(m *model) { m.choosingDatabase = true }},
		{"filter", "FILTER MATERIALIZED VIEWS", func(m *model) { m.filtering = true }},
		{"search", "SEARCH FLOWS", func(m *model) { m.searching = true }},
		{"table picker", "SELECT TABLE", func(m *model) { m.choosingDependency = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{width: 120, height: 30, database: "db", databases: []string{"db"}, overview: true, views: views, allViews: views}
			tc.set(&m)
			view := m.View()
			if !strings.Contains(view, tc.title) || !strings.Contains(view, "╭") || !strings.Contains(view, "ch_insp") {
				t.Fatalf("modal or background missing:\n%s", view)
			}
			lines := strings.Split(view, "\n")
			if len(lines) != m.height {
				t.Fatalf("got %d rows, want %d", len(lines), m.height)
			}
			for _, line := range lines {
				if lipgloss.Width(line) > m.width {
					t.Fatalf("line exceeds terminal: %q", ansi.Strip(line))
				}
			}
		})
	}
}

func TestModalBlocksBackgroundMouseAndFallback(t *testing.T) {
	m := model{width: 120, height: 30, overview: true, choosingDatabase: true, database: "db", databases: []string{"db", "other"}}
	next, _ := m.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !next.(model).choosingDatabase || next.(model).database != "db" {
		t.Fatal("background click affected the open modal")
	}
	next, _ = m.Update(mouseKey("esc"))
	if next.(model).choosingDatabase || !next.(model).overview {
		t.Fatal("escape did not close the selector")
	}
	m.width, m.height = 35, 12
	if m.hasModal() || !strings.Contains(m.View(), "SELECT DATABASE") {
		t.Fatal("small terminal should use the full-screen selector")
	}
}

func TestFilterAndSearchModalHorizontalPadding(t *testing.T) {
	for _, mode := range []string{"filter", "search"} {
		m := model{width: 80, height: 24, overview: true}
		m.filtering = mode == "filter"
		m.searching = mode == "search"
		x, y, width, height := m.modalGeometry()
		lines := strings.Split(m.View(), "\n")
		for row := y + 1; row < y+height-1; row++ {
			line := lines[row]
			if ansi.Cut(line, x+1, x+2) != " " || ansi.Cut(line, x+width-2, x+width-1) != " " {
				t.Fatalf("%s row %d missing horizontal padding: %q", mode, row, ansi.Strip(line))
			}
		}
		next, _ := m.Update(mouseAtText(t, m, "[esc]"))
		if next.(model).filtering || next.(model).searching {
			t.Fatalf("%s close button did not work after padding", mode)
		}
	}
}

func TestWideModalMarginsAndPinnedDetailTitle(t *testing.T) {
	for _, width := range []int{80, 160, 220} {
		m := model{width: width, height: 30, definition: true, views: []clickhouse.View{{Name: "mv", Definition: strings.Repeat("line\n", 40)}}}
		x, _, panelWidth, _ := m.modalGeometry()
		if x != 7 || panelWidth != width-14 {
			t.Fatalf("width %d: margins or panel width incorrect", width)
		}
		m.lineOffset = 10
		if !strings.HasPrefix(ansi.Strip(m.renderDetail(40, 12)), "CREATE DEFINITION") {
			t.Fatal("detail title disappeared when scrolling")
		}
		if !strings.Contains(ansi.Strip(m.View()), "Overview › mv › SQL") {
			t.Fatal("navigation path missing")
		}
	}
}

func TestModalFitsSmallTerminals(t *testing.T) {
	views := []clickhouse.View{{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM src"}}
	for _, size := range [][2]int{{40, 20}, {60, 24}, {80, 30}} {
		for _, selector := range []bool{false, true} {
			m := model{width: size[0], height: size[1], database: "db", views: views, allViews: views}
			if selector {
				m.overview, m.filtering = true, true
			}
			lines := strings.Split(m.View(), "\n")
			if len(lines) != m.height {
				t.Fatalf("%v selector=%v: got %d rows", size, selector, len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > m.width {
					t.Fatalf("%v selector=%v: line too wide: %q", size, selector, ansi.Strip(line))
				}
			}
			click := mouseAtText(t, m, "[esc]")
			next, _ := m.Update(click)
			if selector && next.(model).filtering || !selector && !next.(model).overview {
				t.Fatalf("%v selector=%v: escape footer did not close panel", size, selector)
			}
		}
	}
}
