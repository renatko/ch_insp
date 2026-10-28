package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"chinspector/internal/clickhouse"
)

// overviewGraph groups views by target and draws each target only once.
func overviewGraph(views []clickhouse.View, rates clickhouse.OverviewRates, ready bool, selected int) ([]string, []int) {
	type group struct {
		target  string
		indices []int
	}
	var groups []group
	indices := make(map[string]int)
	for i, view := range views {
		target := clickhouse.InferFlow(view).Target
		key := target
		if target == "internal storage (no TO)" || target == "unknown target" {
			key = fmt.Sprintf("%s\x00%d", target, i)
		}
		index, ok := indices[key]
		if !ok {
			index = len(groups)
			indices[key] = index
			groups = append(groups, group{target: target})
		}
		groups[index].indices = append(groups[index].indices, i)
	}
	var lines []string
	positions := make([]int, len(views))
	for _, group := range groups {
		var block []string
		var connections []int
		groupWidth := 0
		selectedInGroup := false
		for _, i := range group.indices {
			view := views[i]
			positions[i] = len(lines) + len(block)
			marker := "  "
			if i == selected {
				marker = accentStyle.Render("› ")
				selectedInGroup = true
			}
			block = append(block, marker+accentStyle.Render(view.Name))
			viewRates := clickhouse.Rates{Tables: rates.Tables}
			if rate, ok := rates.Views[view.Database+"."+view.Name]; ok {
				viewRates.MVAvailable, viewRates.MVRead, viewRates.MVWritten = true, rate.Read, rate.Written
			}
			flowLines, middle := diagramFlowLines(view, viewRates, ready, false)
			connections = append(connections, len(block)+middle)
			for _, line := range flowLines {
				line = "  " + line
				groupWidth = maxInt(groupWidth, lipgloss.Width(line))
				block = append(block, line)
			}
			block = append(block, "")
		}
		first, last := connections[0], connections[len(connections)-1]
		center := connections[len(connections)/2]
		mvMetric, totalMetric := "MV: no data", "total: no data"
		if !ready {
			mvMetric, totalMetric = "MV: loading", "total: loading"
		} else {
			if !selectedInGroup {
				mvMetric = "MV: select a view"
			} else if rate, ok := rates.Views[views[selected].Database+"."+views[selected].Name]; ok {
				mvMetric = fmt.Sprintf("MV: %.1f rows/s", rate.Written)
			}
			if value, ok := rates.Tables[group.target]; ok {
				totalMetric = fmt.Sprintf("total: %.1f rows/s", value)
			}
		}
		target := diagramBox("TARGET", shortName(group.target), mvMetric+" · "+totalMetric, 0, 0, false, targetStyle)
		for row, content := range block {
			isConnection := false
			for _, pos := range connections {
				if pos == row {
					isConnection = true
					break
				}
			}
			fill := " "
			if isConnection {
				fill = "─"
			}
			content += mutedStyle.Render(strings.Repeat(fill, maxInt(0, groupWidth-lipgloss.Width(content))))
			branch := "     "
			switch {
			case first == last && row == center:
				branch = "────▶"
			case row == center && row == last:
				branch = "──┴─▶"
			case row == center:
				branch = "──┼─▶"
			case row == first:
				branch = "──┐  "
			case row == last:
				branch = "──┘  "
			case isConnection:
				branch = "──┤  "
			case row > first && row < last:
				branch = "  │  "
			}
			content += mutedStyle.Render(branch)
			if row >= center-2 && row < center+3 {
				content += target[row-(center-2)]
			}
			lines = append(lines, content)
		}
	}
	return lines, positions
}
