package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"chinspector/internal/clickhouse"
)

// overviewGraph groups views by target and draws each target only once.
func overviewGraph(views []clickhouse.View, rates clickhouse.OverviewRates, ready bool, selected int, colors map[string]lipgloss.Color) ([]string, []int) {
	type group struct {
		target  string
		key     string
		indices []int
	}
	var groups []group
	indices := make(map[string]int)
	for i, view := range views {
		target := clickhouse.InferFlow(view).Target
		key := overviewGroupKey(view, target)
		index, ok := indices[key]
		if !ok {
			index = len(groups)
			indices[key] = index
			groups = append(groups, group{target: target, key: key})
		}
		groups[index].indices = append(groups[index].indices, i)
	}
	var lines []string
	positions := make([]int, len(views))
	for _, group := range groups {
		groupStyle := lipgloss.NewStyle().Foreground(colors[group.key])
		type row struct {
			index                             int
			inputs, inputStats, name, mvStats string
		}
		var rows []row
		inputWidth, mvWidth := 0, 0
		for _, i := range group.indices {
			view := views[i]
			flow := clickhouse.InferFlow(view)
			sources := flow.Sources
			if len(sources) == 0 {
				sources = []string{"? unknown source"}
			}
			var names, stats []string
			for _, source := range sources {
				name := shortName(source)
				stat := "parts: no data"
				if !ready {
					stat = "parts: loading"
				} else if value, ok := rates.Tables[source]; ok {
					stat = fmt.Sprintf("parts: %.1f rows/s", value)
				}
				width := maxInt(lipgloss.Width(name), lipgloss.Width(stat))
				names = append(names, name+strings.Repeat(" ", width-lipgloss.Width(name)))
				stats = append(stats, stat+strings.Repeat(" ", width-lipgloss.Width(stat)))
			}
			inputs, inputStats := strings.Join(names, " + "), strings.Join(stats, "   ")
			key := view.Database + "." + view.Name
			rate, available := rates.Views[key]
			var history *clickhouse.ViewHistory
			if value, ok := rates.History[key]; ok {
				history = &value
			}
			mvStats := mvRateText(ready, available, rate.Read, rate.Written, history)
			inputWidth = maxInt(inputWidth, maxInt(lipgloss.Width(inputs), lipgloss.Width(inputStats)))
			mvWidth = maxInt(mvWidth, maxInt(lipgloss.Width(view.Name), lipgloss.Width(mvStats)))
			rows = append(rows, row{index: i, inputs: inputs, inputStats: inputStats, name: view.Name, mvStats: mvStats})
		}
		pad := func(value string, width int) string {
			return value + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(value)))
		}
		center := len(rows) / 2
		value, available := rates.Tables[group.target]
		history, hasHistory := rates.TableHistory[group.target]
		total := totalRateText(ready, value, available, history, hasHistory)
		for j, row := range rows {
			positions[row.index] = len(lines)
			rowStyle, statsStyle, markerStyle := groupStyle, mutedStyle, accentStyle
			if row.index == selected {
				rowStyle = rowStyle.Background(selectionBackground)
				statsStyle = statsStyle.Foreground(lipgloss.Color("#E2E8F0")).Background(selectionBackground)
				markerStyle = markerStyle.Background(selectionBackground)
			}
			marker := rowStyle.Render("  ")
			if row.index == selected {
				marker = markerStyle.Render("› ")
			}
			branch := "──┤  "
			switch {
			case len(rows) == 1:
				branch = "────▶"
			case j == center && j == len(rows)-1:
				branch = "──┴─▶"
			case j == center:
				branch = "──┼─▶"
			case j == 0:
				branch = "──┐  "
			case j == len(rows)-1:
				branch = "──┘  "
			}
			line := marker + rowStyle.Bold(true).Render(pad(row.inputs, inputWidth)) + rowStyle.Render(" ─▶ ") + rowStyle.Italic(true).Render(pad(row.name, mvWidth)) + groupStyle.Render(" "+branch)
			if j == center {
				line += groupStyle.Render(" " + shortName(group.target))
			}
			lines = append(lines, line)
			stats := statsStyle.Render("  " + pad(row.inputStats, inputWidth) + "    " + pad(row.mvStats, mvWidth))
			if j < len(rows)-1 {
				stats += groupStyle.Render("   │  ")
			} else {
				stats += "      "
			}
			if j == center {
				stats += mutedStyle.Render(" " + total)
			}
			lines = append(lines, stats)
		}
		lines = append(lines, "")
	}

	return lines, positions
}

// Navigation follows the rendered order, which may differ from catalog order
// because views are grouped by target.
func (m *model) moveOverviewSelection(delta int) {
	if len(m.views) == 0 {
		return
	}
	_, positions := m.overviewLines()
	order := make([]int, len(positions))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return positions[order[i]] < positions[order[j]] })
	for position, index := range order {
		if index != m.selected {
			continue
		}
		next := minInt(maxInt(0, position+delta), len(order)-1)
		if order[next] != m.selected {
			m.selected, m.lineOffset, m.columnOffset, m.metricsReady = order[next], 0, 0, false
			m.keepOverviewSelectionVisible()
		}
		return
	}
}

var overviewPalette = []lipgloss.Color{"#60A5FA", "#FBBF24", "#A78BFA", "#34D399", "#FB7185", "#22D3EE", "#FB923C", "#E879F9"}

func overviewGroupKey(view clickhouse.View, target string) string {
	if target == "internal storage (no TO)" || target == "unknown target" {
		return target + "\x00" + view.Database + "\x00" + view.Name
	}
	return target
}

// Use all views in the active database so filtering does not recolor groups.
func overviewColors(views []clickhouse.View, database string) map[string]lipgloss.Color {
	unique := map[string]bool{}
	var keys []string
	for _, view := range views {
		if view.Database != database {
			continue
		}
		key := overviewGroupKey(view, clickhouse.InferFlow(view).Target)
		if !unique[key] {
			unique[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	colors := make(map[string]lipgloss.Color, len(keys))
	for i, key := range keys {
		colors[key] = overviewPalette[i%len(overviewPalette)]
	}
	return colors
}
