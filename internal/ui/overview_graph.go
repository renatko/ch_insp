package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ch_insp/internal/clickhouse"
)

func compactMetricLabel(label string) string {
	if label == "no events observed" {
		return "no events"
	}
	return label
}

func compactMVRate(ready, available bool, rate clickhouse.ViewRate, history *clickhouse.ViewHistory, diagnostic clickhouse.MetricDiagnostic, readMax, writeMax uint64) string {
	if !ready {
		return mutedStyle.Render("R") + " / " + mutedStyle.Render("W") + " · " + mutedStyle.Render("loading")
	}
	read, write := "R", "W"
	if history != nil {
		read += " " + historyBarScaled(history.Read, history.Events, readMax)
		write += " " + historyBarScaled(history.Written, history.Events, writeMax)
	}
	if available {
		read += fmt.Sprintf(" %.1f", rate.Read)
		write += fmt.Sprintf(" %.1f", rate.Written)
		return readMetricStyle.Render(read) + " / " + writeMetricStyle.Render(write)
	}
	label := compactMetricLabel(metricStateLabel(diagnostic))
	if label == "" {
		label = "no data"
	}
	return readMetricStyle.Render(read) + " / " + writeMetricStyle.Render(write) + " · " + mutedStyle.Render(label)
}

func compactTotalRate(ready bool, value float64, available bool, history clickhouse.TableHistory, hasHistory bool, max uint64, diagnostic clickhouse.MetricDiagnostic) string {
	if !ready {
		return writeMetricStyle.Render("total: loading")
	}
	text := "total:"
	if hasHistory {
		text += " " + historyBarScaled(history.Rows, history.Events, max)
	}
	if available {
		text += fmt.Sprintf(" %.1f", value)
	} else {
		label := compactMetricLabel(metricStateLabel(diagnostic))
		if label == "" {
			label = "no data"
		}
		text += " " + label
	}
	return writeMetricStyle.Render(text)
}

func compactStatusText(status *clickhouse.ViewStatus, err string, now time.Time) string {
	if err != "" {
		return "status unavailable"
	}
	if status == nil || (status.Successes == 0 && status.Errors == 0) {
		return "status ?"
	}
	last := "no ok"
	if status.Successes > 0 {
		age := now.Sub(status.LastSuccess)
		switch {
		case age < 0:
			last = "ok future"
		case age < time.Hour:
			last = fmt.Sprintf("ok %dm", int(age.Minutes()))
		case age < 24*time.Hour:
			last = fmt.Sprintf("ok %dh", int(age.Hours()))
		default:
			last = fmt.Sprintf("ok %dd", int(age.Hours()/24))
		}
	}
	return fmt.Sprintf("%s · err %d", last, status.Errors)
}

// overviewGraph groups views by target and draws each target only once.
func overviewGraph(views []clickhouse.View, rates clickhouse.OverviewRates, ready bool, selected int, colors map[string]lipgloss.Color) ([]string, []int) {
	return overviewGraphFocused(views, rates, ready, selected, colors, false)
}

func overviewFlowArrow(line string) (int, int) {
	for _, arrow := range []string{" ──▶ ", " ─┬▶ "} {
		if i := strings.Index(line, arrow); i >= 0 {
			return i, len(arrow)
		}
	}
	return -1, 0
}

func overviewRowAtLine(views []clickhouse.View, positions []int, line int) (int, int) {
	for i, start := range positions {
		sources := maxInt(1, len(clickhouse.InferFlow(views[i]).Sources))
		if line >= start && line < start+2*sources {
			return i, line - start
		}
	}
	return -1, 0
}

func overviewGraphFocused(views []clickhouse.View, rates clickhouse.OverviewRates, ready bool, selected int, colors map[string]lipgloss.Color, targetFocus bool) ([]string, []int) {
	type row struct {
		index                 int
		inputs, inputStats    []string
		name, mvStats, status string
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
	inputWidth, mvWidth, readWidth, writeWidth, noteWidth := 0, 0, 0, 0, 0
	var readMax, writeMax, totalMax uint64
	if rates.SharedScale {
		readMax, writeMax, totalMax = chartMaxima(views, rates)
	}
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
				indent := ""
				if flow.Trigger != "" {
					if source == flow.Trigger {
						name += " [T]"
					} else {
						name += " [I]"
						indent = "  "
					}
				}
				name = indent + name
				stat := "inserted: no data"
				if !ready {
					stat = "inserted: loading"
				} else if value, ok := rates.Tables[source]; ok {
					stat = fmt.Sprintf("inserted: %.1f", value)
				} else if label := metricStateLabel(rates.TableDiagnostics[source]); label != "" {
					stat = "inserted: " + compactMetricLabel(label)
				}
				stat = indent + readMetricStyle.Render(stat)
				width := maxInt(lipgloss.Width(name), lipgloss.Width(stat))
				names = append(names, name+strings.Repeat(" ", width-lipgloss.Width(name)))
				stats = append(stats, stat+strings.Repeat(" ", width-lipgloss.Width(stat)))
			}
			key := view.Database + "." + view.Name
			rate, available := rates.Views[key]
			var history *clickhouse.ViewHistory
			if value, ok := rates.History[key]; ok {
				history = &value
			}
			mvStats := compactMVRate(ready, available, rate, history, rates.ViewDiagnostics[key], readMax, writeMax)
			if flow.Trigger == "" {
				mvStats += " · " + mutedStyle.Render("trigger ?")
			}
			statusText := ""
			if rates.Statuses != nil || rates.StatusError != "" {
				var status *clickhouse.ViewStatus
				if value, ok := rates.Statuses[key]; ok {
					status = &value
				}
				summary := compactStatusText(status, rates.StatusError, time.Now())
				if status != nil && status.Errors > 0 {
					statusText = errorStyle.Render(summary)
				} else {
					statusText = mutedStyle.Render(summary)
				}
			}
			for n := range names {
				inputWidth = maxInt(inputWidth, maxInt(lipgloss.Width(names[n]), lipgloss.Width(stats[n])))
			}
			if left, right, ok := strings.Cut(mvStats, " / "); ok {
				readWidth = maxInt(readWidth, lipgloss.Width(left))
				write, note, _ := strings.Cut(right, " · ")
				writeWidth = maxInt(writeWidth, lipgloss.Width(write))
				noteWidth = maxInt(noteWidth, lipgloss.Width(note))
			}
			group.rows = append(group.rows, row{index: i, inputs: names, inputStats: stats, name: view.Name, mvStats: mvStats, status: statusText})
		}
	}
	pad := func(value string, width int) string {
		return value + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(value)))
	}
	for i := range groups {
		for j := range groups[i].rows {
			row := &groups[i].rows[j]
			if left, right, ok := strings.Cut(row.mvStats, " / "); ok {
				write, note, hasNote := strings.Cut(right, " · ")
				row.mvStats = pad(left, readWidth) + " / " + pad(write, writeWidth)
				if noteWidth > 0 {
					if hasNote {
						row.mvStats += " · " + pad(note, noteWidth)
					} else {
						row.mvStats += strings.Repeat(" ", 3+noteWidth)
					}
				}
				if row.status != "" {
					row.mvStats += " · " + row.status
				}
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
		total := compactTotalRate(ready, value, available, history, hasHistory, totalMax, rates.TableDiagnostics[group.target])
		targetName := shortName(group.target)
		targetWidth := maxInt(lipgloss.Width(targetName), lipgloss.Width(total))
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
			inputBranch := " ──▶ "
			if len(row.inputs) > 1 {
				inputBranch = " ─┬▶ "
			}
			line := marker + rowStyle.Bold(true).Render(pad(row.inputs[0], inputWidth)) + rowStyle.Render(inputBranch) + rowStyle.Italic(true).Render(pad(row.name, mvWidth)) + groupStyle.Render(" "+branch)
			if j == center {
				targetStyle := groupStyle
				if targetSelected {
					targetStyle = targetStyle.Background(selectionBackground).Bold(true)
				}
				name := "  " + targetName
				if targetSelected {
					name = "› " + pad(targetName, targetWidth)
				}
				line += " " + targetStyle.Render(name)
			}
			lines = append(lines, line)
			inputLink := "     "
			if len(row.inputs) > 1 {
				inputLink = "  │  "
			}
			statsPart := func(text string) string {
				result := statsStyle.Render(text)
				if row.index == selected && !targetFocus {
					result = restoreMetricBackground(result)
				}
				return result
			}
			stats := statsPart("  "+pad(row.inputStats[0], inputWidth)) + rowStyle.Render(inputLink) + statsPart(pad(row.mvStats, mvWidth))
			if j < len(rows)-1 {
				stats += groupStyle.Render("   │  ")
			} else {
				stats += "      "
			}
			if j == center {
				if targetSelected {
					stats += " " + restoreMetricBackground(mutedStyle.Background(selectionBackground).Render("  "+pad(total, targetWidth)))
				} else {
					stats += mutedStyle.Render("   " + total)
				}
			}
			lines = append(lines, stats)
			for source := 1; source < len(row.inputs); source++ {
				link := " ─┤  "
				if source == len(row.inputs)-1 {
					link = " ─┘  "
				}
				extra := rowStyle.Render("  ") + rowStyle.Bold(true).Render(pad(row.inputs[source], inputWidth)) + rowStyle.Render(link+pad("", mvWidth))
				if j < len(rows)-1 {
					extra += groupStyle.Render("   │  ")
				}
				lines = append(lines, extra)
				link = "     "
				if source < len(row.inputs)-1 {
					link = "  │  "
				}
				extraStats := statsPart("  "+pad(row.inputStats[source], inputWidth)) + rowStyle.Render(link) + statsPart(pad("", mvWidth))
				if j < len(rows)-1 {
					extraStats += groupStyle.Render("   │  ")
				}
				lines = append(lines, extraStats)
			}
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
	var totalMax uint64
	if m.chartCommon {
		_, _, totalMax = chartMaxima(m.views, m.overviewRates)
	}
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
		value, available := m.overviewRates.Tables[target]
		history, hasHistory := m.overviewRates.TableHistory[target]
		total := compactTotalRate(m.overviewReady, value, available, history, hasHistory, totalMax, m.overviewRates.TableDiagnostics[target])
		width := maxInt(lipgloss.Width(name), lipgloss.Width(total))
		result = append(result, overviewTargetPosition{key: key, index: index, line: line, column: lipgloss.Width(plain[:at]), width: width})
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
	line := pos.line - 4
	if line < m.overviewScroll || line+1 >= m.overviewScroll+visible {
		m.overviewScroll = line
	}
	width := maxInt(1, m.width)
	left := maxInt(0, pos.column-2) // Include the selection marker.
	if left < m.overviewColumn {
		m.overviewColumn = left
	} else if pos.column+pos.width > m.overviewColumn+width {
		if pos.column+pos.width-left > width {
			m.overviewColumn = left
		} else {
			m.overviewColumn = maxInt(0, pos.column+pos.width-width)
		}
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
