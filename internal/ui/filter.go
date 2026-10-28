package ui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"chinspector/internal/clickhouse"
)

func matchesFlow(view clickhouse.View, source, target string) bool {
	source, target = strings.ToLower(strings.TrimSpace(source)), strings.ToLower(strings.TrimSpace(target))
	if source == "" && target == "" {
		return true
	}
	flow := clickhouse.InferFlow(view)
	if source != "" {
		found := false
		for _, name := range flow.Sources {
			if strings.Contains(strings.ToLower(name), source) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if target != "" {
		if flow.Target == "internal storage (no TO)" || flow.Target == "unknown target" {
			return false
		}
		if !strings.Contains(strings.ToLower(flow.Target), target) {
			return false
		}
	}
	return true
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filtering = false
		return m, nil
	case "tab", "shift+tab", "up", "down":
		m.filterField = 1 - m.filterField
		return m, nil
	case "enter":
		return m.applyFilter(m.draftSource, m.draftTarget)
	case "ctrl+x":
		return m.applyFilter("", "")
	case "ctrl+u":
		if m.filterField == 0 {
			m.draftSource = ""
		} else {
			m.draftTarget = ""
		}
		return m, nil
	case "backspace", "ctrl+h":
		if m.filterField == 0 {
			m.draftSource = deleteLastRune(m.draftSource)
		} else {
			m.draftTarget = deleteLastRune(m.draftTarget)
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		var text strings.Builder
		for _, r := range msg.Runes {
			if !unicode.IsControl(r) {
				text.WriteRune(r)
			}
		}
		if m.filterField == 0 {
			m.draftSource += text.String()
		} else {
			m.draftTarget += text.String()
		}
	}
	return m, nil
}

func deleteLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func (m model) applyFilter(source, target string) (tea.Model, tea.Cmd) {
	m.filterSource, m.filterTarget = strings.TrimSpace(source), strings.TrimSpace(target)
	m.filtering = false
	m.selected, m.lineOffset, m.columnOffset, m.overviewScroll, m.overviewColumn = 0, 0, 0, 0, 0
	m.metricsReady, m.overviewReady = false, false
	m.filterViews()
	if len(m.views) == 0 {
		return m, nil
	}
	if m.overview && !m.overviewBusy {
		m.overviewBusy = true
		return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision)
	}
	if !m.overview && !m.metricsBusy {
		m.metricsBusy = true
		return m, loadMetrics(m.dsn, m.views[m.selected])
	}
	return m, nil
}

func (m model) renderFilter(width int) string {
	field := func(label, value string, focus bool) string {
		prefix := "  "
		if focus {
			prefix = "› "
		}
		if value == "" {
			value = "(any)"
		}
		line := ansi.Truncate(prefix+label+": "+value, maxInt(1, width), "…")
		if focus {
			return accentStyle.Render(line)
		}
		return mutedStyle.Render(line)
	}
	return strings.Join([]string{
		accentStyle.Render("FILTER MATERIALIZED VIEWS"),
		mutedStyle.Render("Case-insensitive substring; both fields must match."), "",
		field("SOURCE", m.draftSource, m.filterField == 0), "",
		field("TARGET", m.draftTarget, m.filterField == 1), "",
		mutedStyle.Render("Tab / ↑ / ↓: switch field · Ctrl+U: clear field"),
		mutedStyle.Render("Uses inferred FROM/JOIN and TO names from the SQL definition."), "",
		mutedStyle.Render(ansi.Truncate("[enter] apply  [ctrl+x] clear filters  [esc] cancel", maxInt(1, width), "…")),
	}, "\n")
}
