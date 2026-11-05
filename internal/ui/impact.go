package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ch_insp/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) startImpact(column string) tea.Cmd {
	m.cancelColumns()
	m.columnsRequest++
	request, revision := m.columnsRequest, m.viewRevision
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	m.columnsCancel = cancel
	views := append([]clickhouse.View(nil), m.allViews...)
	if m.allViews == nil {
		views = append(views, m.views...)
	}
	table, dsn, dump := m.dependencyTable, m.dsn, m.dump
	m.impact, m.impactLoading, m.impactColumn = true, true, column
	m.impactFilter, m.impactFiltering = "", false
	m.impactResult = clickhouse.ImpactResult{}
	m.dependencyCursor, m.lineOffset, m.columnOffset = 0, 0, 0
	return func() tea.Msg {
		defer cancel()
		loader := func(ctx context.Context, view clickhouse.View) (clickhouse.ColumnLineage, error) {
			if dump != nil {
				return dump.ColumnLineage(view)
			}
			return clickhouse.LoadColumnLineage(ctx, dsn, view)
		}
		result := clickhouse.AnalyzeImpact(ctx, table, column, views, loader)
		if dump != nil && len(dump.Warnings) > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("SQL dump has %d import warnings; catalog may be incomplete.", len(dump.Warnings)))
		}
		return impactLoadedMsg{request: request, revision: revision, result: result}
	}
}

func (m model) impactItems() []dependencyItem {
	all := make([]dependencyItem, 0, len(m.impactResult.Items))
	for _, edge := range m.impactResult.Items {
		indent := strings.Repeat("  ", edge.Depth)
		if edge.Status == "target" {
			name := edge.Table
			if edge.Column != "" {
				name += "." + edge.Column
			}
			suffix := ""
			if edge.Usage != "" {
				suffix = " [" + edge.Usage + "]"
			}
			if edge.Cycle {
				suffix += " [cycle; path stops]"
			}
			all = append(all, dependencyItem{label: indent + "→ " + name + suffix})
			continue
		}
		view := edge.View
		label := indent + "→ " + view.Database + "." + view.Name + " [" + edge.Role + "]"
		if edge.Usage != "" {
			label += " [" + edge.Usage + "]"
		}
		if edge.Status == "possible" || edge.Status == "unresolved" {
			label += " [" + edge.Status + "]"
		}
		if edge.Expression != "" {
			label += " · " + edge.Expression
		}
		all = append(all, dependencyItem{label: label, view: &view})
	}
	items := make([]dependencyItem, 0, len(all))
	if m.impactFilter != "" {
		keep := make([]bool, len(all))
		stack := []int{}
		for i, item := range all {
			depth := m.impactResult.Items[i].Depth
			for len(stack) > 0 && m.impactResult.Items[stack[len(stack)-1]].Depth >= depth {
				stack = stack[:len(stack)-1]
			}
			if strings.Contains(strings.ToLower(item.label), strings.ToLower(m.impactFilter)) {
				keep[i] = true
				for _, parent := range stack {
					keep[parent] = true
				}
			}
			stack = append(stack, i)
		}
		for i, item := range all {
			if keep[i] {
				items = append(items, item)
			}
		}
		return items
	}
	return all
}

func (m model) impactLines() []string {
	name := m.dependencyTable
	if m.impactColumn != "" {
		name += "." + m.impactColumn
	}
	help := "SQL-inferred · all accessible views · may be incomplete"
	if m.impactFilter != "" || m.impactFiltering {
		help += " · filter: " + m.impactFilter
	}
	if m.impactFiltering {
		help += "_ [enter] done · [ctrl+x] clear"
	}
	lines := []string{accentStyle.Render("DOWNSTREAM IMPACT"), targetStyle.Render(name), mutedStyle.Render(help), ""}
	if m.impactLoading {
		return append(lines, loadingText("analyzing dependencies…"))
	}
	items := m.impactItems()
	if len(items) == 0 {
		message := "No dependencies found in accessible definitions."
		if m.impactFilter != "" {
			message = "No paths match the filter."
		}
		lines = append(lines, emptyText(message))
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
	if len(m.impactResult.Warnings) > 0 {
		lines = append(lines, "", errorStyle.Render("Analysis may be incomplete:"))
		for _, warning := range m.impactResult.Warnings {
			lines = append(lines, errorStyle.Render("! "+warning))
		}
	}
	return append(lines, "", mutedStyle.Render("[↑]/[↓] select · [enter] open MV · [/] filter · [esc] back"))
}
