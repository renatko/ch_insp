package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"chinspector/internal/clickhouse"
)

type filterOption struct{ value, label string }

func matchesFlow(view clickhouse.View, source, target string) bool {
	if source == "" && target == "" {
		return true
	}
	flow := clickhouse.InferFlow(view)
	if source != "" {
		found := false
		for _, name := range flow.Sources {
			if name == source {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return target == "" || (flow.Target != "internal storage (no TO)" && flow.Target != "unknown target" && flow.Target == target)
}

// Options are collected from all discovered views, not just filtered results.
func (m model) filterOptions(field int) []filterOption {
	unique := map[string]bool{}
	for _, view := range m.allViews {
		if view.Database != m.database {
			continue
		}
		flow := clickhouse.InferFlow(view)
		if field == 0 {
			for _, name := range flow.Sources {
				unique[name] = true
			}
		} else if flow.Target != "unknown target" && flow.Target != "internal storage (no TO)" {
			unique[flow.Target] = true
		}
	}
	current := m.draftSource
	if field == 1 {
		current = m.draftTarget
	}
	missing := current != "" && !unique[current]
	if missing {
		unique[current] = true
	}
	var names []string
	counts := map[string]int{}
	for name := range unique {
		names = append(names, name)
		counts[shortName(name)]++
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := strings.ToLower(shortName(names[i])), strings.ToLower(shortName(names[j]))
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
	options := []filterOption{{label: "(any table)"}}
	for _, name := range names {
		label := shortName(name)
		if counts[label] > 1 {
			if i := strings.IndexByte(name, '.'); i >= 0 {
				label += "  [database: " + name[:i] + "]"
			}
		}
		if missing && name == current {
			label += "  (not in current database)"
		}
		options = append(options, filterOption{value: name, label: label})
	}
	return options
}

func (m *model) openFilter() {
	m.filtering = true
	m.draftSource, m.draftTarget = m.filterSource, m.filterTarget
	m.filterField = 0
	for field := 0; field < 2; field++ {
		selected := m.draftSource
		if field == 1 {
			selected = m.draftTarget
		}
		m.filterCursors[field] = 0
		for i, option := range m.filterOptions(field) {
			if option.value == selected {
				m.filterCursors[field] = i
				break
			}
		}
	}
}

func (m *model) selectFilterOption(field, index int) {
	options := m.filterOptions(field)
	if index < 0 || index >= len(options) {
		return
	}
	m.filterField, m.filterCursors[field] = field, index
	if field == 0 {
		m.draftSource = options[index].value
	} else {
		m.draftTarget = options[index].value
	}
	selected := options[index].value
	for i, option := range m.filterOptions(field) {
		if option.value == selected {
			m.filterCursors[field] = i
			break
		}
	}
}

func (m model) filterVisible() int { return maxInt(1, m.height-11) }
func (m model) filterStart() int {
	return maxInt(0, m.filterCursors[m.filterField]-m.filterVisible()+1)
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	field := m.filterField
	options := m.filterOptions(field)
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filtering = false
	case "tab", "shift+tab", "left", "right":
		m.filterField = 1 - field
	case "up", "k":
		m.filterCursors[field] = maxInt(0, m.filterCursors[field]-1)
	case "down", "j":
		m.filterCursors[field] = minInt(len(options)-1, m.filterCursors[field]+1)
	case "pgup":
		m.filterCursors[field] = maxInt(0, m.filterCursors[field]-m.filterVisible())
	case "pgdown":
		m.filterCursors[field] = minInt(len(options)-1, m.filterCursors[field]+m.filterVisible())
	case "home":
		m.filterCursors[field] = 0
	case "end":
		m.filterCursors[field] = len(options) - 1
	case " ":
		m.selectFilterOption(field, m.filterCursors[field])
	case "enter":
		m.selectFilterOption(field, m.filterCursors[field])
		return m.applyFilter(m.draftSource, m.draftTarget)
	case "ctrl+x":
		return m.applyFilter("", "")
	}
	return m, nil
}

func (m model) applyFilter(source, target string) (tea.Model, tea.Cmd) {
	m.filterSource, m.filterTarget = source, target
	m.filtering = false
	m.selected, m.lineOffset, m.columnOffset, m.overviewScroll, m.overviewColumn = 0, 0, 0, 0, 0
	m.metricsReady, m.overviewReady = false, false
	m.filterViews()
	if len(m.views) == 0 {
		return m, nil
	}
	if m.overview && !m.overviewBusy {
		m.overviewBusy = true
		return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision)
	}
	if !m.overview && !m.metricsBusy {
		m.metricsBusy = true
		return m, loadMetrics(m.dsn, m.views[m.selected])
	}
	return m, nil
}

func (m model) renderFilter(width int) string {
	chosen := func(value string) string {
		if value == "" {
			return "any"
		}
		return shortName(value)
	}
	current := m.draftSource
	label := "SOURCE TABLES"
	if m.filterField == 1 {
		current, label = m.draftTarget, "TARGET TABLES"
	}
	tabs := fmt.Sprintf("[source] %s    [target] %s", chosen(m.draftSource), chosen(m.draftTarget))
	options := m.filterOptions(m.filterField)
	start := m.filterStart()
	end := minInt(len(options), start+m.filterVisible())
	lines := []string{accentStyle.Render("FILTER MATERIALIZED VIEWS"), mutedStyle.Render("Choose one source and/or target table."), ansi.Truncate(tabs, maxInt(1, width), "…"), "", accentStyle.Render(fmt.Sprintf("%s  %d/%d", label, m.filterCursors[m.filterField]+1, len(options)))}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.filterCursors[m.filterField] {
			prefix = "› "
		}
		check := "[ ] "
		if options[i].value == current {
			check = "[x] "
		}
		line := ansi.Truncate(prefix+check+options[i].label, maxInt(1, width), "…")
		if i == m.filterCursors[m.filterField] {
			lines = append(lines, activeStyle.Render(line))
		} else {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "", mutedStyle.Render(ansi.Truncate("Tab switch · ↑/↓ move · Space select", maxInt(1, width), "…")), mutedStyle.Render(ansi.Truncate("[enter] apply  [ctrl+x] clear filters  [esc] cancel", maxInt(1, width), "…")))
	return strings.Join(lines, "\n")
}
