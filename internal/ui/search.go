package ui

import (
	"fmt"
	"strings"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type searchResult struct {
	kind, name string
	index      int
}

func (m model) searchResults() []searchResult {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if q == "" {
		return nil
	}
	matches := func(s string) bool { return strings.Contains(strings.ToLower(s), q) }
	var result []searchResult
	seenTargets := map[string]bool{}
	for i, v := range m.views {
		flow := clickhouse.InferFlow(v)
		for _, source := range flow.Sources {
			if matches(source) {
				result = append(result, searchResult{kind: "source", name: source, index: i})
			}
		}
		name := v.Database + "." + v.Name
		if matches(name) {
			result = append(result, searchResult{kind: "MV", name: name, index: i})
		}
		key := overviewGroupKey(v, flow.Target)
		if !seenTargets[key] && flow.Target != "unknown target" && flow.Target != "internal storage (no TO)" {
			seenTargets[key] = true
			if matches(flow.Target) {
				result = append(result, searchResult{kind: "target", name: flow.Target, index: i})
			}
		}
	}
	return result
}
func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	results := m.searchResults()
	switch msg.String() {
	case "esc":
		m.searching = false
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "up":
		m.searchCursor = maxInt(0, m.searchCursor-1)
	case "down":
		m.searchCursor = minInt(maxInt(0, len(results)-1), m.searchCursor+1)
	case "backspace", "ctrl+h":
		r := []rune(m.searchQuery)
		if len(r) > 0 {
			m.searchQuery = string(r[:len(r)-1])
		}
		m.searchCursor = 0
	case " ":
		m.searchQuery += " "
		m.searchCursor = 0
	case "enter":
		if len(results) == 0 {
			return m, nil
		}
		r := results[minInt(m.searchCursor, len(results)-1)]
		// Search always lands in overview. Cancel any details request and
		// discard its mode so Enter opens the selected result's diagram.
		m.cancelColumns()
		m.columns = false
		m.target = false
		m.failuresMode = false
		m.dependencies = false
		m.definition = false
		m.searching = false
		m.overview = true
		m.overviewTargetFocus = r.kind == "target"
		m.selected = r.index
		m.metricsReady = false
		if m.overviewTargetFocus {
			m.keepOverviewTargetVisible()
		} else {
			m.keepOverviewSelectionVisible()
			// Bring the matching name into the horizontal viewport.
			lines, positions := m.overviewLines()
			if r.index < len(positions) {
				text := ansi.Strip(lines[positions[r.index]])
				name := shortName(r.name)
				at := strings.Index(text, name)
				if r.kind == "MV" {
					if arrow := strings.Index(text, " ─▶ "); arrow >= 0 {
						start := arrow + len(" ─▶ ")
						if offset := strings.Index(text[start:], m.views[r.index].Name); offset >= 0 {
							at = start + offset
						} else {
							at = -1
						}
					}
				}
				if at >= 0 {
					x := ansi.StringWidth(text[:at])
					if x < m.overviewColumn || x+ansi.StringWidth(name) > m.overviewColumn+maxInt(1, m.width) {
						m.overviewColumn = maxInt(0, x)
					}
				}
			}
		}
		if !m.overviewReady && !m.overviewBusy && len(m.views) > 0 {
			m.overviewBusy = true
			return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.searchQuery += string(msg.Runes)
			m.searchCursor = 0
		}
	}
	return m, nil
}
func (m model) renderSearch(width int) string {
	results := m.searchResults()
	position := 0
	if len(results) > 0 {
		position = minInt(m.searchCursor, len(results)-1) + 1
	}
	lines := []string{accentStyle.Render("SEARCH FLOWS"), "/ " + m.searchQuery + "▏", mutedStyle.Render(fmt.Sprintf("%d/%d matches in filtered views", position, len(results))), ""}
	visible := maxInt(1, m.height-10)
	start := maxInt(0, m.searchCursor-visible+1)
	end := minInt(len(results), start+visible)
	for i := start; i < end; i++ {
		r := results[i]
		line := fmt.Sprintf("  %-7s %s", r.kind, r.name)
		if r.kind == "source" {
			line += "  → " + m.views[r.index].Name
		}
		line = ansi.Truncate(line, maxInt(1, width), "…")
		if i == m.searchCursor {
			line = activeStyle.Render("›" + strings.TrimPrefix(line, " "))
		}
		lines = append(lines, line)
	}
	if len(results) == 0 {
		lines = append(lines, mutedStyle.Render("No matches."))
	}
	lines = append(lines, "", mutedStyle.Render("↑/↓ select  [enter] jump  [esc] cancel"))
	return strings.Join(lines, "\n")
}
