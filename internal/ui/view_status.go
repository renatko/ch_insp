package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
)

type failuresLoadedMsg struct {
	key      string
	revision int
	request  uint64
	failures []clickhouse.ViewFailure
	err      error
}

func statusHoursLabel(hours int) string {
	switch hours {
	case 1:
		return "1h"
	case 168:
		return "7d"
	default:
		return "24h"
	}
}
func statusText(status *clickhouse.ViewStatus, err string, hours int) string {
	window := statusHoursLabel(hours)
	if err != "" {
		return "status unavailable"
	}
	if status == nil || (status.Successes == 0 && status.Errors == 0) {
		return "status unknown (" + window + ")"
	}
	last := "no success observed"
	if status.Successes > 0 {
		last = "last ok " + status.LastSuccess.Format("15:04:05")
	}
	return fmt.Sprintf("%s · errors %d (%s, observed)", last, status.Errors, window)
}
func (m *model) loadFailures() tea.Cmd {
	if len(m.views) == 0 {
		return nil
	}
	m.cancelColumns()
	view, revision, dsn := m.views[m.selected], m.viewRevision, m.dsn
	hours := m.statusWindowHours()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.columnsCancel = cancel
	m.columnsRequest++
	request := m.columnsRequest
	m.failuresLoading = true
	m.failuresError = nil
	m.failureSelected = 0
	m.failureDetail = false
	return func() tea.Msg {
		defer cancel()
		failures, err := clickhouse.LoadViewFailures(ctx, dsn, view, hours)
		return failuresLoadedMsg{key: view.Database + "\x00" + view.Name, revision: revision, request: request, failures: failures, err: err}
	}
}
func (m model) failureLines() []string {
	lines := []string{accentStyle.Render("MV ERRORS · " + m.views[m.selected].Name), mutedStyle.Render("Last " + m.statusWindowLabel() + " · local node · up to 50 events · w: window · e/esc: back"), ""}
	if m.failuresLoading {
		return append(lines, mutedStyle.Render("Loading errors…"))
	}
	if m.failuresError != nil {
		return append(lines, errorStyle.Render(m.failuresError.Error()))
	}
	if len(m.failures) == 0 {
		return append(lines, mutedStyle.Render("No observed errors; logging may be disabled or delayed."))
	}
	if m.failureDetail {
		f := m.failures[m.failureSelected]
		lines = append(lines, fmt.Sprintf("%s  %s  code %d", f.Time.Format("2006-01-02 15:04:05"), f.Status, f.Code), "Query ID: "+f.QueryID, "", "Message:")
		lines = append(lines, strings.Split(f.Message, "\n")...)
		return append(lines, "", mutedStyle.Render("Enter: list · PgUp/PgDn: scroll"))
	}
	for i, f := range m.failures {
		message := strings.ReplaceAll(strings.ReplaceAll(f.Message, "\n", " "), "\r", " ")
		prefix := "  "
		if i == m.failureSelected {
			prefix = "› "
		}
		line := fmt.Sprintf("%s%s  %s  code %d  %s", prefix, f.Time.Format("2006-01-02 15:04:05"), f.Status, f.Code, message)
		if i == m.failureSelected {
			lines = append(lines, activeStyle.Render(line))
		} else {
			lines = append(lines, line)
		}
	}
	return append(lines, "", mutedStyle.Render("↑/↓: select · Enter: full message"))
}
