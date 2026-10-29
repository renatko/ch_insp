package ui

import (
	"ch_insp/internal/clickhouse"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
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
			flow := clickhouse.InferFlow(d.View)
			for _, source := range flow.Sources {
				role := "input; trigger unknown"
				if flow.Trigger != "" {
					if source == flow.Trigger {
						role = "inferred trigger"
					} else {
						role = "other query input"
					}
				}
				items = append(items, dependencyItem{label: "    ← table " + source + " (" + role + ")", table: source})
			}
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
	return append(lines, "", mutedStyle.Render("↑/↓ select · Enter open · t: columns · Backspace previous · g/esc back"))
}
func (m *model) openDependencies() {
	if len(m.views) == 0 {
		return
	}
	flow := clickhouse.InferFlow(m.views[m.selected])
	table := ""
	if m.target && m.targetExplicit != "" {
		table = m.targetExplicit
	} else if m.target && m.targetData.Table != "" {
		table = m.targetData.Database + "." + m.targetData.Table
	} else if m.overviewTargetFocus {
		table = flow.Target
	} else if len(flow.Sources) > 1 {
		m.dependencyChoices = append([]string(nil), flow.Sources...)
		if flow.Target != "" && flow.Target != "unknown target" && flow.Target != "internal storage (no TO)" && !contains(m.dependencyChoices, flow.Target) {
			m.dependencyChoices = append(m.dependencyChoices, flow.Target)
		}
		m.dependencyChoice = 0
		m.choosingDependency = true
		return
	} else if len(flow.Sources) > 0 {
		table = flow.Sources[0]
	} else {
		table = flow.Target
	}
	if table == "" || table == "unknown target" || table == "internal storage (no TO)" {
		return
	}
	m.openDependencyTable(table)
}

func (m *model) openDependencyTable(table string) {
	m.choosingDependency = false
	m.dependencyReturnScreen = "diagram"
	switch {
	case m.overview:
		m.dependencyReturnScreen = "overview"
	case m.target:
		m.dependencyReturnScreen = "target"
	case m.columns:
		m.dependencyReturnScreen = "columns"
	case m.definition:
		m.dependencyReturnScreen = "definition"
	case m.failuresMode:
		m.dependencyReturnScreen = "errors"
	}
	m.cancelColumns()
	m.dependencyReturnTarget = m.target
	m.dependencyReturnTargetExplicit = m.targetExplicit
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
		if m.dependencyReturnTarget {
			m.target = true
			m.targetExplicit = m.dependencyReturnTargetExplicit
			m.dependencyReturnTargetExplicit = ""
			m.dependencyReturnTarget = false
			m.lineOffset = 0
			return m, m.loadTarget(), true
		}
		m.lineOffset = 0
		switch m.dependencyReturnScreen {
		case "overview":
			m.overview = true
		case "columns":
			m.columns = true
			return m, m.loadColumns(), true
		case "definition":
			m.definition = true
		case "errors":
			m.failuresMode = true
			return m, m.loadFailures(), true
		}
		return m, nil, true
	case "t":
		if m.dependencyTable == "" {
			return m, nil, true
		}
		m.dependencies = false
		m.target = true
		m.targetReturnDependencies = true
		m.targetReturnOverview = false
		m.targetExplicit = m.dependencyTable
		m.lineOffset = 0
		m.columnOffset = 0
		return m, m.loadTarget(), true
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
			visible := false
			for _, v := range m.views {
				if v.Database == item.view.Database && v.Name == item.view.Name {
					visible = true
					break
				}
			}
			if !visible {
				m.database = item.view.Database
				m.filterSource = nil
				m.filterTarget = nil
				m.filterViews()
			}
			for i, v := range m.views {
				if v.Database == item.view.Database && v.Name == item.view.Name {
					m.selected = i
					m.dependencies = false
					m.lineOffset = 0
					m.metricsReady = false
					m.metricsBusy = true
					return m, loadMetrics(m.dsn, v, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch), true
				}
			}
			m.dependencyMessage = "The selected view is no longer in the catalog. Refresh and try again."
			return m, nil, true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m model) updateDependencyPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "g":
		m.choosingDependency = false
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		m.dependencyChoice = maxInt(0, m.dependencyChoice-1)
	case "down", "j":
		m.dependencyChoice = minInt(maxInt(0, len(m.dependencyChoices)-1), m.dependencyChoice+1)
	case "enter":
		if len(m.dependencyChoices) > 0 {
			m.openDependencyTable(m.dependencyChoices[m.dependencyChoice])
		}
	}
	return m, nil
}
func (m model) dependencyPickerVisible() int { return maxInt(1, m.height-9) }
func (m model) dependencyPickerStart() int {
	return maxInt(0, m.dependencyChoice-m.dependencyPickerVisible()+1)
}
func (m model) renderDependencyPicker(width int) string {
	lines := []string{accentStyle.Render("SELECT TABLE"), mutedStyle.Render("Choose an input or target to inspect dependencies."), ""}
	shortCounts := map[string]int{}
	for _, name := range m.dependencyChoices {
		shortCounts[shortName(name)]++
	}
	start := m.dependencyPickerStart()
	end := minInt(len(m.dependencyChoices), start+m.dependencyPickerVisible())
	for i := start; i < end; i++ {
		name := shortName(m.dependencyChoices[i])
		if shortCounts[name] > 1 {
			name = m.dependencyChoices[i]
		}
		line := ansi.Truncate(fmt.Sprintf("  %s", name), maxInt(1, width), "…")
		if i == m.dependencyChoice {
			line = activeStyle.Render("›" + strings.TrimPrefix(line, " "))
		}
		lines = append(lines, line)
	}
	return strings.Join(append(lines, "", mutedStyle.Render("↑/↓ select  [enter] open  [esc] cancel")), "\n")
}
