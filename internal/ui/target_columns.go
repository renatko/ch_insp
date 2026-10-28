package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
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
	return func() tea.Msg {
		defer cancel()
		data, err := clickhouse.LoadTargetColumns(ctx, dsn, view, catalog)
		return targetLoadedMsg{key: view.Database + "\x00" + view.Name, revision: revision, request: request, data: data, err: err}
	}
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
	lines = append(lines, targetStyle.Render(shortName(data.Database+"."+data.Table)), mutedStyle.Render(fmt.Sprintf("%d columns · %d visible materialized views", len(data.Columns), len(data.Views))), "")
	for _, c := range data.Columns {
		heading := c.Name + " (" + c.Type + ")"
		if c.DefaultKind != "" {
			heading += "  " + c.DefaultKind + " " + c.DefaultExpression
		}
		lines = append(lines, targetStyle.Render(heading))
		if len(c.Writers) == 0 {
			lines = append(lines, mutedStyle.Render("  No identified MV output"), "")
			continue
		}
		for _, w := range c.Writers {
			name := w.View
			if strings.HasPrefix(name, m.database+".") {
				name = strings.TrimPrefix(name, m.database+".")
			}
			lines = append(lines, viewStyle.Render("  ← "+name), "    SQL: "+w.Expression)
			for _, input := range w.Inputs {
				lines = append(lines, "    "+styleColumnPath(input))
			}
		}
		lines = append(lines, "")
	}
	for _, warning := range data.Warnings {
		lines = append(lines, errorStyle.Render("! "+warning))
	}
	lines = append(lines, mutedStyle.Render("Based on visible MV definitions; direct inserts and hidden views are not included."))
	return lines
}
