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
			mvStats := "read/write: no data"
			if !ready {
				mvStats = "read/write: loading"
			} else if rate, ok := rates.Views[view.Database+"."+view.Name]; ok {
				mvStats = fmt.Sprintf("read %.1f / write %.1f rows/s", rate.Read, rate.Written)
			}
			inputWidth = maxInt(inputWidth, maxInt(lipgloss.Width(inputs), lipgloss.Width(inputStats)))
			mvWidth = maxInt(mvWidth, maxInt(lipgloss.Width(view.Name), lipgloss.Width(mvStats)))
			rows = append(rows, row{index: i, inputs: inputs, inputStats: inputStats, name: view.Name, mvStats: mvStats})
		}
		pad := func(value string, width int) string {
			return value + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(value)))
		}
		center := len(rows) / 2
		total := "total: no data"
		if !ready {
			total = "total: loading"
		} else if value, ok := rates.Tables[group.target]; ok {
			total = fmt.Sprintf("total: %.1f rows/s", value)
		}
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
			line := marker + rowStyle.Bold(true).Render(pad(row.inputs, inputWidth)) + rowStyle.Render(" ─▶ ") + rowStyle.Italic(true).Render(pad(row.name, mvWidth)) + rowStyle.Render(" "+branch)
			if j == center {
				line += rowStyle.Render(" " + shortName(group.target))
			}
			lines = append(lines, line)
			stats := statsStyle.Render("  " + pad(row.inputStats, inputWidth) + "    " + pad(row.mvStats, mvWidth) + " ")
			if j < len(rows)-1 {
				stats += rowStyle.Render("  │  ")
			} else {
				stats += statsStyle.Render("     ")
			}
			if j == center {
				stats += statsStyle.Render(" " + total)
			}
			lines = append(lines, stats)
		}
		lines = append(lines, "")
	}

	return lines, positions
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
