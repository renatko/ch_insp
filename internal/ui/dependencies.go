package ui

import (
	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
)

type dependencyItem struct {
	label, table string
	view         *clickhouse.View
}

func (m model) dependencyItems() []dependencyItem {
	views := m.allViews
	if views == nil {
		views = m.views
	}
	deps := clickhouse.TableDependencies(m.dependencyTable, views)
	var items []dependencyItem
	for _, d := range deps {
		name := d.View.Database + "." + d.View.Name
		if d.Direction == "writes" {
			v := d.View
			items = append(items, dependencyItem{label: "← MV " + name + " writes to table", view: &v})
		} else {
			v := d.View
			items = append(items, dependencyItem{label: "→ MV " + name + " reads (" + d.Role + ")", view: &v})
			if d.Other != "" && d.Other != "unknown target" && d.Other != "internal storage (no TO)" {
				items = append(items, dependencyItem{label: "    → table " + d.Other, table: d.Other})
			}
		}
	}
	return items
}
func (m model) dependencyLines() []string {
	lines := []string{accentStyle.Render("TABLE DEPENDENCIES"), targetStyle.Render(m.dependencyTable), mutedStyle.Render("SQL-inferred · all visible schemas · may be incomplete"), ""}
	items := m.dependencyItems()
	if len(items) == 0 {
		lines = append(lines, mutedStyle.Render("No visible inferred relationships."))
	}
	for i, item := range items {
		prefix := "  "
		if i == m.dependencyCursor {
			prefix = "› "
		}
		line := prefix + item.label
		if i == m.dependencyCursor {
			line = activeStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return append(lines, "", mutedStyle.Render("↑/↓ select · Enter open · Backspace previous · g/esc back"))
}
func (m *model) openDependencies() {
	if len(m.views) == 0 {
		return
	}
	flow := clickhouse.InferFlow(m.views[m.selected])
	table := ""
	if m.target && m.targetData.Table != "" {
		table = m.targetData.Database + "." + m.targetData.Table
	} else if m.overviewTargetFocus {
		table = flow.Target
	} else if len(flow.Sources) > 0 {
		table = flow.Sources[0]
	} else {
		table = flow.Target
	}
	if table == "" || table == "unknown target" || table == "internal storage (no TO)" {
		return
	}
	m.cancelColumns()
	m.dependencyReturnOverview = m.overview
	m.overview = false
	m.target = false
	m.columns = false
	m.definition = false
	m.failuresMode = false
	m.dependencies = true
	m.dependencyTable = table
	m.dependencyHistory = nil
	m.dependencyCursor = 0
	m.dependencyMessage = ""
	m.lineOffset = 0
	m.columnOffset = 0
}
func (m model) updateDependencyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.dependencies || m.overview {
		return m, nil, false
	}
	items := m.dependencyItems()
	switch msg.String() {
	case "g", "esc":
		m.dependencies = false
		m.overview = m.dependencyReturnOverview
		m.lineOffset = 0
		return m, nil, true
	case "backspace":
		if len(m.dependencyHistory) > 0 {
			m.dependencyTable = m.dependencyHistory[len(m.dependencyHistory)-1]
			m.dependencyHistory = m.dependencyHistory[:len(m.dependencyHistory)-1]
			m.dependencyCursor = 0
			m.lineOffset = 0
		}
		return m, nil, true
	case "up", "k":
		m.dependencyCursor = maxInt(0, m.dependencyCursor-1)
		m.lineOffset = maxInt(0, 4+m.dependencyCursor-maxInt(1, m.height-12))
		return m, nil, true
	case "down", "j":
		m.dependencyCursor = minInt(maxInt(0, len(items)-1), m.dependencyCursor+1)
		m.lineOffset = maxInt(0, 4+m.dependencyCursor-maxInt(1, m.height-12))
		return m, nil, true
	case "enter":
		if len(items) == 0 {
			return m, nil, true
		}
		item := items[minInt(m.dependencyCursor, len(items)-1)]
		if item.table != "" {
			m.dependencyHistory = append(m.dependencyHistory, m.dependencyTable)
			m.dependencyTable = item.table
			m.dependencyCursor = 0
			m.lineOffset = 0
			return m, nil, true
		}
		if item.view != nil {
			// An explicit jump may leave the active filter or database. Clear
			// filters so the requested visible-catalog view can be opened.
			m.database = item.view.Database
			m.filterSource = nil
			m.filterTarget = nil
			m.filterViews()
			for i, v := range m.views {
				if v.Database == item.view.Database && v.Name == item.view.Name {
					m.selected = i
					m.dependencies = false
					m.lineOffset = 0
					m.metricsReady = false
					m.metricsBusy = true
					return m, loadMetrics(m.dsn, v), true
				}
			}
			m.dependencyMessage = "The selected view is no longer in the catalog. Refresh and try again."
			return m, nil, true
		}
		return m, nil, true
	}
	return m, nil, false
}
