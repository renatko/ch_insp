package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"chinspector/internal/clickhouse"
)

// overviewGraph groups views by target and draws each target only once.
func overviewGraph(views []clickhouse.View, rates clickhouse.OverviewRates, ready bool, selected int, colors map[string]lipgloss.Color) ([]string, []int) {
	return overviewGraphFocused(views, rates, ready, selected, colors, false)
}

func overviewGraphFocused(views []clickhouse.View, rates clickhouse.OverviewRates, ready bool, selected int, colors map[string]lipgloss.Color, targetFocus bool) ([]string, []int) {
	type row struct {
		index                             int
		inputs, inputStats, name, mvStats string
	}
	type group struct {
		target  string
		key     string
		indices []int
		rows    []row
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
	inputWidth, mvWidth, readWidth := 0, 0, 0
	for groupIndex := range groups {
		group := &groups[groupIndex]
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
				stat := "inserted: no data"
				if !ready {
					stat = "inserted: loading"
				} else if value, ok := rates.Tables[source]; ok {
					stat = fmt.Sprintf("inserted: %.1f rows/s", value)
				}
				stat = readMetricStyle.Render(stat)
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
			if left, _, ok := strings.Cut(mvStats, " / "); ok {
				readWidth = maxInt(readWidth, lipgloss.Width(left))
			}
			group.rows = append(group.rows, row{index: i, inputs: inputs, inputStats: inputStats, name: view.Name, mvStats: mvStats})
		}
	}
	pad := func(value string, width int) string {
		return value + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(value)))
	}
	for i := range groups {
		for j := range groups[i].rows {
			row := &groups[i].rows[j]
			if left, right, ok := strings.Cut(row.mvStats, " / "); ok {
				row.mvStats = pad(left, readWidth) + " / " + right
			}
			mvWidth = maxInt(mvWidth, maxInt(lipgloss.Width(row.name), lipgloss.Width(row.mvStats)))
		}
	}
	var lines []string
	positions := make([]int, len(views))
	for _, group := range groups {
		groupStyle := lipgloss.NewStyle().Foreground(colors[group.key])
		rows := group.rows
		center := len(rows) / 2
		targetSelected := false
		if targetFocus {
			for _, index := range group.indices {
				if index == selected {
					targetSelected = true
					break
				}
			}
		}
		value, available := rates.Tables[group.target]
		history, hasHistory := rates.TableHistory[group.target]
		total := totalRateText(ready, value, available, history, hasHistory)
		for j, row := range rows {
			positions[row.index] = len(lines)
			rowStyle, statsStyle, markerStyle := groupStyle, mutedStyle, accentStyle
			if row.index == selected && !targetFocus {
				rowStyle = rowStyle.Background(selectionBackground)
				statsStyle = statsStyle.Foreground(lipgloss.Color("#E2E8F0")).Background(selectionBackground)
				markerStyle = markerStyle.Background(selectionBackground)
			}
			marker := rowStyle.Render("  ")
			if row.index == selected && !targetFocus {
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
				targetStyle := groupStyle
				if targetSelected {
					targetStyle = targetStyle.Background(selectionBackground).Bold(true)
				}
				line += " " + targetStyle.Render(shortName(group.target))
			}
			lines = append(lines, line)
			stats := statsStyle.Render("  " + pad(row.inputStats, inputWidth) + "    " + pad(row.mvStats, mvWidth))
			if row.index == selected && !targetFocus {
				stats = restoreMetricBackground(stats)
			}
			if j < len(rows)-1 {
				stats += groupStyle.Render("   │  ")
			} else {
				stats += "      "
			}
			if j == center {
				if targetSelected {
					stats += " " + restoreMetricBackground(mutedStyle.Background(selectionBackground).Render(total))
				} else {
					stats += mutedStyle.Render(" " + total)
				}
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

// Nested foreground styles reset terminal attributes; restore the selected
// background after those resets, leaving the final reset at the row boundary.
func restoreMetricBackground(value string) string {
	const reset = "\x1b[0m"
	last := strings.LastIndex(value, reset)
	if last < 0 {
		return value
	}
	probe := lipgloss.NewStyle().Background(selectionBackground).Render("X")
	index := strings.Index(probe, "X")
	if index <= 0 {
		return value
	}
	return strings.ReplaceAll(value[:last], reset, reset+probe[:index]) + value[last:]
}

// Overview target positions refer to the central row of each target group.
type overviewTargetPosition struct {
	key                        string
	index, line, column, width int
}

func (m model) overviewTargets() []overviewTargetPosition {
	if len(m.views) == 0 {
		return nil
	}
	lines, positions := m.overviewLines()
	groups := map[string][]int{}
	var keys []string
	for i, v := range m.views {
		target := clickhouse.InferFlow(v).Target
		key := overviewGroupKey(v, target)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], i)
	}
	var result []overviewTargetPosition
	for _, key := range keys {
		indices := groups[key]
		target := clickhouse.InferFlow(m.views[indices[0]]).Target
		if target == "unknown target" || target == "internal storage (no TO)" {
			continue
		}
		index := indices[len(indices)/2]
		line := positions[index]
		if line >= len(lines) {
			continue
		}
		plain := ansi.Strip(lines[line])
		name := shortName(target)
		at := strings.LastIndex(plain, name)
		if at < 0 {
			continue
		}
		result = append(result, overviewTargetPosition{key: key, index: index, line: line, column: lipgloss.Width(plain[:at]), width: lipgloss.Width(name)})
	}
	return result
}
func (m *model) selectedOverviewTarget() (overviewTargetPosition, bool) {
	if len(m.views) == 0 {
		return overviewTargetPosition{}, false
	}
	view := m.views[m.selected]
	key := overviewGroupKey(view, clickhouse.InferFlow(view).Target)
	for _, pos := range m.overviewTargets() {
		if pos.key == key {
			return pos, true
		}
	}
	return overviewTargetPosition{}, false
}
func (m *model) keepOverviewTargetVisible() {
	pos, ok := m.selectedOverviewTarget()
	if !ok {
		return
	}
	visible := maxInt(1, m.height-7)
	if pos.line < m.overviewScroll || pos.line+1 >= m.overviewScroll+visible {
		m.overviewScroll = pos.line
	}
	width := maxInt(1, m.width)
	if pos.column < m.overviewColumn {
		m.overviewColumn = pos.column
	} else if pos.column+pos.width > m.overviewColumn+width {
		m.overviewColumn = maxInt(0, pos.column+pos.width-width)
	}
}
func (m *model) moveOverviewTarget(delta int) {
	targets := m.overviewTargets()
	current, ok := m.selectedOverviewTarget()
	if !ok {
		return
	}
	for i, target := range targets {
		if target.key == current.key {
			next := minInt(maxInt(0, i+delta), len(targets)-1)
			m.selected = targets[next].index
			m.keepOverviewTargetVisible()
			return
		}
	}
}
