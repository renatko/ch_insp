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

func matchesFlow(view clickhouse.View, source, target []string) bool {
	if len(source) == 0 && len(target) == 0 {
		return true
	}
	flow := clickhouse.InferFlow(view)
	if len(source) > 0 {
		found := false
		for _, name := range flow.Sources {
			if contains(source, name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(target) == 0 || (flow.Target != "internal storage (no TO)" && flow.Target != "unknown target" && contains(target, flow.Target))
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
	missing := map[string]bool{}
	for _, name := range current {
		if !unique[name] {
			missing[name] = true
			unique[name] = true
		}
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
		if missing[name] {
			label += "  (not in current database)"
		}
		options = append(options, filterOption{value: name, label: label})
	}
	return options
}

func (m *model) openFilter() {
	m.filtering = true
	m.draftSource, m.draftTarget = append([]string(nil), m.filterSource...), append([]string(nil), m.filterTarget...)
	m.filterField = 0
	for field := 0; field < 2; field++ {
		selected := m.draftSource
		if field == 1 {
			selected = m.draftTarget
		}
		m.filterCursors[field] = 0
		for i, option := range m.filterOptions(field) {
			if contains(selected, option.value) {
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
	current := m.draftSource
	if field == 1 {
		current = m.draftTarget
	}
	selected := options[index].value
	var next []string
	if selected == "" {
		if !allFilterOptionsSelected(options, current) {
			for _, option := range options[1:] {
				next = append(next, option.value)
			}
		}
	} else {
		for _, name := range current {
			if name != selected {
				next = append(next, name)
			}
		}
		if !contains(current, selected) {
			next = append(next, selected)
		}
	}
	if field == 0 {
		m.draftSource = next
	} else {
		m.draftTarget = next
	}
	newOptions := m.filterOptions(field)
	m.filterCursors[field] = minInt(index, len(newOptions)-1)
	for i, option := range newOptions {
		if option.value == selected {
			m.filterCursors[field] = i
			break
		}
	}
}

func allFilterOptionsSelected(options []filterOption, selected []string) bool {
	if len(options) <= 1 {
		return false
	}
	for _, option := range options[1:] {
		if !contains(selected, option.value) {
			return false
		}
	}
	return true
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
		m.cancelColumns()
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
		return m.applyFilter(m.draftSource, m.draftTarget)
	case "ctrl+x":
		return m.applyFilter(nil, nil)
	}
	return m, nil
}

func (m model) applyFilter(source, target []string) (tea.Model, tea.Cmd) {
	m.cancelColumns()
	m.filterSource, m.filterTarget = append([]string(nil), source...), append([]string(nil), target...)
	m.filtering = false
	m.selected, m.lineOffset, m.columnOffset, m.overviewScroll, m.overviewColumn = 0, 0, 0, 0, 0
	m.metricsReady, m.overviewReady = false, false
	m.filterViews()
	if len(m.views) == 0 {
		return m, nil
	}
	if m.overview && !m.overviewBusy {
		m.overviewBusy = true
		return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.chartEpoch)
	}
	if !m.overview && !m.metricsBusy {
		m.metricsBusy = true
		cmd := m.columnCmdIfActive()
		return m, tea.Batch(loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.chartEpoch), cmd)
	}
	if !m.overview {
		cmd := m.columnCmdIfActive()
		return m, cmd
	}
	return m, nil
}

func (m model) renderFilter(width int) string {
	chosen := func(values []string) string {
		if len(values) == 0 {
			return "any"
		}
		return fmt.Sprintf("%d selected", len(values))
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
	lines := []string{accentStyle.Render("FILTER MATERIALIZED VIEWS"), mutedStyle.Render("Select multiple tables; matches any selection in each list."), ansi.Truncate(tabs, maxInt(1, width), "…"), "", accentStyle.Render(fmt.Sprintf("%s  %d/%d", label, m.filterCursors[m.filterField]+1, len(options)))}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.filterCursors[m.filterField] {
			prefix = "› "
		}
		check := "[ ] "
		if (options[i].value == "" && allFilterOptionsSelected(options, current)) || (options[i].value != "" && contains(current, options[i].value)) {
			check = "[x] "
		}
		line := ansi.Truncate(prefix+check+options[i].label, maxInt(1, width), "…")
		if i == m.filterCursors[m.filterField] {
			lines = append(lines, activeStyle.Render(line))
		} else {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "", mutedStyle.Render(ansi.Truncate("Tab switch · ↑/↓ move · Space toggle", maxInt(1, width), "…")), mutedStyle.Render(ansi.Truncate("[enter] apply  [ctrl+x] clear filters  [esc] cancel", maxInt(1, width), "…")))
	return strings.Join(lines, "\n")
}
