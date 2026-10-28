package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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
			start := minInt(m.overviewScroll, maxInt(0, len(lines)-visible))
			line := start + msg.Y - 2
			allViews := m.allViews
			if allViews == nil {
				allViews = m.views
			}
			graph, _ := overviewGraph(m.views, m.overviewRates, m.overviewReady, m.selected, overviewColors(allViews, m.database))
			if msg.Y >= 2 && msg.Y < 2+visible && line >= 4 && line < 4+len(graph) {
				index, best := -1, -1
				for i, position := range starts {
					if position <= line && position > best {
						index, best = i, position
					}
				}
				if index >= 0 {
					if index == m.selected {
						return m.Update(mouseKey("enter"))
					}
					m.selected, m.lineOffset, m.columnOffset, m.metricsReady = index, 0, 0, false
				}
			}
			return m, nil
		}
		if m.width >= 70 && msg.X >= 0 && msg.X < maxInt(26, m.width/3) && msg.Y >= 4 {
			limit := maxInt(1, maxInt(5, m.height-8)-2)
			start := maxInt(0, m.selected-limit+1)
			index := start + msg.Y - 4
			if index >= start && index < minInt(len(m.views), start+limit) {
				if m.selectView(index) && m.columns {
					cmd := m.loadColumns()
					return m, cmd
				}
			}
		}
		return m, nil
	}
	if m.loading || m.err != nil {
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
			return m.Update(mouseKey("left"))
		}
		return m.Update(mouseKey("right"))
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
		m.overviewScroll = minInt(maxInt(0, m.overviewScroll+delta), maxInt(0, len(lines)-maxInt(1, m.height-7)))
		return m, nil
	}
	if m.width >= 70 && msg.X < maxInt(26, m.width/3) {
		if len(m.views) > 0 {
			if m.selectView(minInt(maxInt(0, m.selected+delta), len(m.views)-1)) && m.columns {
				cmd := m.loadColumns()
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
	keys := []string{"c", "f", "o", "enter", "b", "d", "r", "esc", "ctrl+x", "q"}
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
