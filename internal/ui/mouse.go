package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone {
		if m.overview && !m.loading && m.err == nil && !m.searching && !m.filtering && !m.choosingDatabase {
			m.hoverOverview(msg.X, msg.Y)
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft {
		if key := m.clickedAction(msg.X, msg.Y); key != "" {
			return m.Update(mouseKey(key))
		}
		if m.loading || m.err != nil {
			return m, nil
		}
		if m.searching {
			results := m.searchResults()
			visible := maxInt(1, m.height-10)
			start := maxInt(0, m.searchCursor-visible+1)
			index := start + msg.Y - 6
			if msg.Y >= 6 && index >= start && index < minInt(len(results), start+visible) {
				if index == m.searchCursor {
					return m.updateSearch(mouseKey("enter"))
				}
				m.searchCursor = index
			}
			return m, nil
		}
		if m.filtering {
			if msg.Y == 4 {
				lines := strings.Split(m.View(), "\n")
				line := ansi.Strip(lines[4])
				at := strings.Index(line, "[target]")
				m.filterField = 0
				if at >= 0 && msg.X >= lipgloss.Width(line[:at]) {
					m.filterField = 1
				}
			} else if msg.Y >= 7 && msg.Y < 7+m.filterVisible() {
				m.selectFilterOption(m.filterField, m.filterStart()+msg.Y-7)
			}
			return m, nil
		}
		if m.choosingDatabase {
			limit := maxInt(1, m.height-8)
			start := maxInt(0, m.databaseCursor-limit+1)
			index := start + msg.Y - 4
			if msg.Y >= 4 && msg.Y < 4+minInt(limit, len(m.databases)-start) && index >= 0 && index < len(m.databases) {
				m.databaseCursor = index
				return m.Update(mouseKey("enter"))
			}
			return m, nil
		}
		if m.overview && len(m.views) > 0 {
			lines, starts := m.overviewLines()
			visible := maxInt(1, m.height-7)
			start := minInt(m.overviewScroll, maxInt(0, len(lines)-4-visible))
			line := start + msg.Y - 2
			allViews := m.allViews
			if allViews == nil {
				allViews = m.views
			}
			graph, _ := overviewGraph(m.views, m.overviewRates, m.overviewReady, m.selected, overviewColors(allViews, m.database))
			if msg.Y >= 6 && msg.Y < 6+visible && line >= 4 && line < 4+len(graph) {
				for _, target := range m.overviewTargets() {
					x := msg.X + m.overviewColumn
					if (line == target.line || line == target.line+1) && x >= target.column && x < target.column+target.width {
						if m.overviewTargetFocus {
							current, ok := m.selectedOverviewTarget()
							if ok && current.key == target.key {
								return m.Update(mouseKey("enter"))
							}
						}
						m.selected = target.index
						m.overviewTargetFocus = true
						m.keepOverviewTargetVisible()
						return m, nil
					}
				}
				index, best := -1, -1
				for i, position := range starts {
					if position <= line && position > best {
						index, best = i, position
					}
				}
				if index >= 0 {
					if m.overviewTargetFocus {
						m.overviewTargetFocus = false
						m.selected = index
						return m, nil
					}
					if index == m.selected {
						return m.Update(mouseKey("enter"))
					}
					m.selected, m.lineOffset, m.columnOffset, m.metricsReady = index, 0, 0, false
				}
			}
			return m, nil
		}
		if m.failuresMode && !m.failuresLoading && !m.failureDetail && m.failuresError == nil && (m.width < 70 || msg.X >= maxInt(26, m.width/3)+1) {
			contentTop := 3
			height := maxInt(5, m.height-8)
			if m.width < 70 {
				contentTop = 4
				height = maxInt(5, m.height-9)
			}
			lines := m.detailLines()
			offset := minInt(m.lineOffset, maxInt(0, len(lines)-maxInt(1, height-2)))
			index := msg.Y - contentTop + offset - 3
			if msg.Y >= contentTop && msg.Y < contentTop+height-2 && index >= 0 && index < len(m.failures) {
				if index == m.failureSelected {
					m.failureDetail = true
					m.lineOffset = 0
				} else {
					m.failureSelected = index
				}
				return m, nil
			}
		}
		if m.target && !m.targetLoading && m.targetError == nil && (m.width < 70 || msg.X >= maxInt(26, m.width/3)+1) {
			contentTop := 3
			height := maxInt(5, m.height-8)
			if m.width < 70 {
				contentTop = 4
				height = maxInt(5, m.height-9)
			}
			lines := m.detailLines()
			offset := minInt(m.lineOffset, maxInt(0, len(lines)-maxInt(1, height-2)))
			start, end := m.targetColumnWindow()
			index := start + msg.Y - contentTop + offset - 4
			if msg.Y >= contentTop && msg.Y < contentTop+height-2 && index >= start && index < end {
				m.targetColumn = index
				m.lineOffset = 0
				return m, nil
			}
		}
		if m.width >= 70 && msg.X >= 0 && msg.X < maxInt(26, m.width/3) && msg.Y >= 4 {
			limit := maxInt(1, maxInt(5, m.height-8)-2)
			start := maxInt(0, m.selected-limit+1)
			index := start + msg.Y - 4
			if index >= start && index < minInt(len(m.views), start+limit) {
				if m.selectView(index) && (m.columns || m.target || m.failuresMode) {
					cmd := m.columnCmdIfActive()
					return m, cmd
				}
			}
		}
		return m, nil
	}
	if m.loading || m.err != nil {
		return m, nil
	}
	if m.searching {
		delta := 0
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -3
		}
		if msg.Button == tea.MouseButtonWheelDown {
			delta = 3
		}
		m.searchCursor = minInt(maxInt(0, m.searchCursor+delta), maxInt(0, len(m.searchResults())-1))
		return m, nil
	}
	if m.filtering {
		delta := 0
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -3
		}
		if msg.Button == tea.MouseButtonWheelDown {
			delta = 3
		}
		if delta != 0 {
			field := m.filterField
			m.filterCursors[field] = minInt(maxInt(0, m.filterCursors[field]+delta), len(m.filterOptions(field))-1)
		}
		return m, nil
	}
	if msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelRight || (msg.Shift && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown)) {
		if msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelUp {
			return m.Update(mouseKey("H"))
		}
		return m.Update(mouseKey("L"))
	}
	delta := 0
	if msg.Button == tea.MouseButtonWheelUp {
		delta = -3
	}
	if msg.Button == tea.MouseButtonWheelDown {
		delta = 3
	}
	if delta == 0 {
		return m, nil
	}
	if m.choosingDatabase {
		m.databaseCursor = minInt(maxInt(0, m.databaseCursor+delta), maxInt(0, len(m.databases)-1))
		return m, nil
	}
	if m.overview {
		lines, _ := m.overviewLines()
		m.overviewScroll = minInt(maxInt(0, m.overviewScroll+delta), maxInt(0, len(lines)-4-maxInt(1, m.height-7)))
		return m, nil
	}
	if m.width >= 70 && msg.X < maxInt(26, m.width/3) {
		if len(m.views) > 0 {
			if m.selectView(minInt(maxInt(0, m.selected+delta), len(m.views)-1)) && (m.columns || m.target || m.failuresMode) {
				cmd := m.columnCmdIfActive()
				return m, cmd
			}
		}
		return m, nil
	}
	if len(m.views) > 0 {
		height := maxInt(5, m.height-8)
		if m.width < 70 {
			height = maxInt(5, m.height-9)
		}
		m.lineOffset = minInt(maxInt(0, m.lineOffset+delta), maxInt(0, len(m.detailLines())-maxInt(1, height-2)))
	}
	return m, nil
}

func (m *model) selectView(index int) bool {
	if m.selected != index {
		m.selected, m.lineOffset, m.columnOffset, m.metricsReady = index, 0, 0, false
		return true
	}
	return false
}

func mouseKey(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "ctrl+x":
		return tea.KeyMsg{Type: tea.KeyCtrlX}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// Hit-test the visible footer text so clipped controls are never clickable.
func (m model) clickedAction(x, y int) string {
	lines := strings.Split(m.View(), "\n")
	if y != len(lines)-1 || y < 0 {
		return ""
	}
	line := ansi.Strip(lines[y])
	keys := []string{"/", "e", "t", "c", "f", "o", "enter", "b", "d", "r", "esc", "ctrl+x", "q"}
	for _, key := range keys {
		marker := "[" + key + "]"
		at := strings.Index(line, marker)
		if at < 0 {
			continue
		}
		end := len(line)
		if next := strings.Index(line[at+len(marker):], "  "); next >= 0 {
			end = at + len(marker) + next
		}
		startX, endX := lipgloss.Width(line[:at]), lipgloss.Width(line[:end])
		if x >= startX && x < endX {
			return key
		}
	}
	return ""
}

// Hover only changes focus within actual diagram rows; it never scrolls or
// opens anything, so moving across an empty area cannot change selection.
func (m *model) hoverOverview(x, y int) {
	if len(m.views) == 0 {
		return
	}
	lines, positions := m.overviewLines()
	visible := maxInt(1, m.height-7)
	if y < 6 || y >= 6+visible {
		return
	}
	start := minInt(m.overviewScroll, maxInt(0, len(lines)-4-visible))
	line := start + y - 2
	if line < 0 || line >= len(lines) {
		return
	}
	column := x + m.overviewColumn
	for _, target := range m.overviewTargets() {
		if (line == target.line || line == target.line+1) && column >= target.column && column < target.column+target.width {
			m.selected = target.index
			m.overviewTargetFocus = true
			return
		}
	}
	for i, pos := range positions {
		if line != pos && line != pos+1 {
			continue
		}
		flow := ansi.Strip(lines[pos])
		arrow := strings.Index(flow, " ─▶ ")
		if arrow < 0 {
			continue
		}
		after := arrow + len(" ─▶ ")
		name := strings.Index(flow[after:], m.views[i].Name)
		if name < 0 {
			continue
		}
		end := lipgloss.Width(flow[:after+name+len(m.views[i].Name)])
		if column >= 0 && column < end {
			if m.selected != i {
				m.metricsReady = false
			}
			m.selected = i
			m.overviewTargetFocus = false
		}
		return
	}
}
