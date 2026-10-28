package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type targetLoadedMsg struct {
	key      string
	revision int
	request  uint64
	data     clickhouse.TargetColumns
	err      error
}

func (m *model) loadTarget() tea.Cmd {
	if len(m.views) == 0 {
		return nil
	}
	m.cancelColumns()
	view, revision, dsn := m.views[m.selected], m.viewRevision, m.dsn
	catalog := append([]clickhouse.View(nil), m.allViews...)
	if catalog == nil {
		catalog = append(catalog, m.views...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	m.columnsCancel = cancel
	m.columnsRequest++
	request := m.columnsRequest
	m.targetLoading, m.targetError = true, nil
	m.targetColumn = 0
	return func() tea.Msg {
		defer cancel()
		data, err := clickhouse.LoadTargetColumns(ctx, dsn, view, catalog)
		return targetLoadedMsg{key: view.Database + "\x00" + view.Name, revision: revision, request: request, data: data, err: err}
	}
}

// targetColumnWindow keeps the selected row and its details visible on small screens.
func (m model) targetColumnWindow() (int, int) {
	count := len(m.targetData.Columns)
	visible := maxInt(1, (m.height-16)/2)
	if m.height <= 0 {
		visible = 6
	}
	if visible > count {
		visible = count
	}
	start := maxInt(0, m.targetColumn-visible+1)
	if start+visible > count {
		start = maxInt(0, count-visible)
	}
	return start, start + visible
}

func (m model) targetLines() []string {
	lines := []string{accentStyle.Render("TARGET TABLE"), mutedStyle.Render("Potential MV outputs · matched by column name · t: back"), ""}
	if m.targetLoading {
		return append(lines, mutedStyle.Render("Loading target columns and materialized views…"))
	}
	if m.targetError != nil {
		return append(lines, errorStyle.Render("Cannot inspect target: "+m.targetError.Error()))
	}
	data := m.targetData
	lines = append(lines, targetStyle.Render(shortName(data.Database+"."+data.Table))+mutedStyle.Render(fmt.Sprintf("  ·  %d columns · %d visible MVs", len(data.Columns), len(data.Views))))
	start, end := m.targetColumnWindow()
	for i := start; i < end; i++ {
		c := data.Columns[i]
		names := []string{}
		seen := map[string]bool{}
		for _, w := range c.Writers {
			if seen[w.View] {
				continue
			}
			seen[w.View] = true
			name := w.View
			if strings.HasPrefix(name, m.database+".") {
				name = strings.TrimPrefix(name, m.database+".")
			}
			names = append(names, name)
		}
		prefix := "  "
		if i == m.targetColumn {
			prefix = "› "
		}
		base := prefix + c.Name + "  "
		shortType := ansi.Truncate(c.Type, 20, "…")
		row := base + shortType
		suffix := "  · no MV"
		if c.DefaultKind != "" {
			suffix += " · " + c.DefaultKind
		}
		if len(names) > 0 {
			suffix = "  ← " + strings.Join(names, ", ")
		}
		if lipgloss.Width(row+suffix) > m.detailWidth() && len(names) > 0 {
			suffix = fmt.Sprintf("  ← %d MV", len(names))
		}
		available := m.detailWidth() - lipgloss.Width(base+suffix)
		if available > 0 && lipgloss.Width(shortType) > available {
			shortType = ansi.Truncate(c.Type, available, "…")
		}
		row = base + shortType + suffix
		if i == m.targetColumn {
			lines = append(lines, activeStyle.Render(row))
		} else {
			lines = append(lines, row)
		}
	}
	if start > 0 || end < len(data.Columns) {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("  columns %d–%d of %d", start+1, end, len(data.Columns))))
	}
	lines = append(lines, "")
	if len(data.Columns) > 0 && m.targetColumn >= 0 && m.targetColumn < len(data.Columns) {
		c := data.Columns[m.targetColumn]
		lines = append(lines, accentStyle.Render(c.Name), "  TARGET TYPE: "+c.Type)
		if c.DefaultKind != "" {
			lines = append(lines, mutedStyle.Render("  "+c.DefaultKind+" "+c.DefaultExpression))
		}
		if len(c.Writers) == 0 {
			lines = append(lines, mutedStyle.Render("  No identified MV output"))
		}
		for _, w := range c.Writers {
			name := w.View
			if strings.HasPrefix(name, m.database+".") {
				name = strings.TrimPrefix(name, m.database+".")
			}
			lines = append(lines, viewStyle.Render("  ← "+name), "    SQL: "+w.Expression)
			if w.ViewType != "" && c.Type != "" && w.ViewType != c.Type {
				lines = append(lines, "    MV TYPE: "+w.ViewType+" → target: "+c.Type)
			}
			for _, input := range w.Inputs {
				lines = append(lines, "    "+styleColumnPath(input))
			}
		}
	}
	for _, warning := range data.Warnings {
		lines = append(lines, errorStyle.Render("! "+warning))
	}
	lines = append(lines, "", mutedStyle.Render("↑/↓: select column · PgUp/PgDn: scroll details"), mutedStyle.Render("Visible MV definitions only; direct inserts and hidden views are not included."))
	return lines
}
