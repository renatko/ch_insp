package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ch_insp/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
)

type observationLoadedMsg struct {
	mode, key   string
	revision    int
	request     uint64
	executions  []clickhouse.ViewExecution
	diagnostics clickhouse.FlowDiagnostics
	err         error
}

func (m *model) loadObservation(mode string) tea.Cmd {
	m.cancelColumns()
	m.columnsRequest++
	request, revision := m.columnsRequest, m.viewRevision
	view, dsn, hours := m.views[m.selected], m.dsn, m.observationWindowHours()
	catalog := append([]clickhouse.View(nil), m.allViews...)
	if m.allViews == nil {
		catalog = append(catalog, m.views...)
	}
	offline := m.schemaPath != ""
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	m.columnsCancel = cancel
	m.observationMode, m.observationLoading, m.observationError = mode, true, nil
	m.observationExecutions = nil
	m.observationDiagnostics = clickhouse.FlowDiagnostics{}
	m.observationSelected, m.lineOffset, m.columnOffset = 0, 0, 0
	m.observationDetail = false
	return func() tea.Msg {
		defer cancel()
		msg := observationLoadedMsg{mode: mode, key: view.Database + "\x00" + view.Name, revision: revision, request: request}
		if mode == "executions" {
			if offline {
				msg.err = fmt.Errorf("offline: execution history is unavailable in a SQL dump")
			} else {
				msg.executions, msg.err = clickhouse.LoadViewExecutions(ctx, dsn, view, hours)
			}
		} else if offline {
			msg.diagnostics = clickhouse.StaticFlowDiagnostics(view, catalog)
			msg.diagnostics.Offline = true
		} else {
			msg.diagnostics = clickhouse.LoadFlowDiagnostics(ctx, dsn, view, catalog)
		}
		return msg
	}
}
func (m model) updateObservationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		if m.observationMode == "executions" && m.observationDetail {
			m.observationDetail = false
			m.lineOffset = 0
			return m, nil, true
		}
		m.cancelColumns()
		m.columnsRequest++
		m.observationMode = ""
		m.overview = m.observationReturnOverview
		m.lineOffset, m.columnOffset = 0, 0
		return m, nil, true
	case "q", "ctrl+c":
		m.cancelColumns()
		return m, tea.Quit, true
	case "x", "a":
		mode := "executions"
		if msg.String() == "a" {
			mode = "diagnostics"
		}
		if mode == m.observationMode {
			return m, nil, true
		}
		cmd := m.loadObservation(mode)
		return m, cmd, true
	case "r":
		cmd := m.loadObservation(m.observationMode)
		return m, cmd, true
	case "w":
		if m.observationMode == "executions" {
			m.observationWindow = (m.observationWindow + 1) % 3
			cmd := m.loadObservation("executions")
			return m, cmd, true
		}
		return m, nil, true
	case "up", "k":
		if m.observationMode == "executions" && !m.observationDetail {
			m.observationSelected = maxInt(0, m.observationSelected-1)
			m.lineOffset = maxInt(0, 3+m.observationSelected-maxInt(1, m.height-12))
		} else {
			m.lineOffset = maxInt(0, m.lineOffset-1)
		}
		return m, nil, true
	case "down", "j":
		if m.observationMode == "executions" && !m.observationDetail {
			m.observationSelected = minInt(maxInt(0, len(m.observationExecutions)-1), m.observationSelected+1)
			m.lineOffset = maxInt(0, 3+m.observationSelected-maxInt(1, m.height-12))
		} else {
			m.lineOffset = minInt(m.lineOffset+1, maxInt(0, len(m.detailLines())-m.detailVisibleRows()))
		}
		return m, nil, true
	case "enter":
		if m.observationMode == "executions" && !m.observationLoading && len(m.observationExecutions) > 0 {
			m.observationDetail = !m.observationDetail
			m.lineOffset = 0
		}
		return m, nil, true
	case "pgup", "pgdown", "left", "right", "h", "l", "H", "L", "shift+left", "shift+right":
		return m, nil, false
	}
	return m, nil, true
}

func (m model) observationLines() []string {
	if m.observationMode == "diagnostics" {
		return m.flowDiagnosticsLines()
	}
	lines := []string{accentStyle.Render("MV EXECUTIONS · " + m.views[m.selected].Name), mutedStyle.Render("Last " + statusHoursLabel(m.observationWindowHours()) + " · local node · up to 50 events · [w] window"), ""}
	if m.observationLoading {
		return append(lines, loadingText("execution history…"))
	}
	if m.observationError != nil {
		return append(lines, errorText(m.observationError.Error()))
	}
	if len(m.observationExecutions) == 0 {
		return append(lines, emptyText("No terminal events observed; logging may be disabled, sampled or delayed."))
	}
	if m.observationDetail {
		e := m.observationExecutions[m.observationSelected]
		lines = append(lines, fmt.Sprintf("%s  %s", e.Time.Format("2006-01-02 15:04:05"), e.Status), "Query ID: "+e.QueryID)
		if e.Status == "QueryFinish" {
			lines = append(lines, fmt.Sprintf("Duration: %d ms · read: %d rows · written: %d rows", e.DurationMS, e.ReadRows, e.WrittenRows))
		} else {
			lines = append(lines, fmt.Sprintf("Exception code: %d", e.Code), "", "Message:")
			lines = append(lines, strings.Split(e.Message, "\n")...)
		}
		return append(lines, "", mutedStyle.Render("[enter]/[esc] list · [PgUp]/[PgDn] scroll"))
	}
	for i, e := range m.observationExecutions {
		prefix := "  "
		if i == m.observationSelected {
			prefix = "› "
		}
		summary := fmt.Sprintf("%6d ms  R %-10d W %-10d", e.DurationMS, e.ReadRows, e.WrittenRows)
		if e.Status != "QueryFinish" {
			summary = fmt.Sprintf("code %d  %s", e.Code, strings.ReplaceAll(strings.ReplaceAll(e.Message, "\n", " "), "\r", " "))
		}
		line := fmt.Sprintf("%s%s  %s  %s", prefix, e.Time.Format("2006-01-02 15:04:05"), e.Status, summary)
		if i == m.observationSelected {
			line = activeStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return append(lines, "", mutedStyle.Render("Terminal log events only · logging may be sampled or delayed."))
}

func (m model) observationWindowHours() int {
	switch m.observationWindow {
	case 1:
		return 1
	case 2:
		return 168
	default:
		return 24
	}
}

func (m model) flowDiagnosticsLines() []string {
	lines := []string{accentStyle.Render("FLOW DIAGNOSTICS · " + m.views[m.selected].Name), mutedStyle.Render("Local node · last 5 minutes · observed log events"), ""}
	if m.observationLoading {
		return append(lines, loadingText("flow observations…"))
	}
	d := m.observationDiagnostics
	lines = append(lines, accentStyle.Render("SOURCES"))
	if len(d.Sources) == 0 {
		lines = append(lines, mutedStyle.Render("  Source not inferred; inspect SQL."))
	}
	for _, s := range d.Sources {
		role := "input; trigger unknown"
		if d.Flow.Trigger == s.Table {
			role = "inferred trigger"
		} else if d.Flow.Trigger != "" {
			role = "other query input"
		}
		lines = append(lines, "  "+s.Table+" ["+role+"]", "    "+partObservationText(s, d.Offline))
	}
	lines = append(lines, "", accentStyle.Render("MATERIALIZED VIEW"))
	switch {
	case d.Offline:
		lines = append(lines, mutedStyle.Render("  Execution logs unavailable offline."))
	case d.ViewError != "":
		lines = append(lines, errorStyle.Render("  "+d.ViewError))
	default:
		lines = append(lines, fmt.Sprintf("  Successful executions: %d · failed executions: %d", d.Successes, d.Failures))
		if d.Successes > 0 {
			lines = append(lines, "  Last success: "+d.LastSuccess.Format("2006-01-02 15:04:05"))
		}
		if d.Failures > 0 {
			lines = append(lines, "  Last failure: "+d.LastFailure.Format("2006-01-02 15:04:05"))
		}
	}
	lines = append(lines, "", accentStyle.Render("TARGET"))
	if d.Target == nil {
		lines = append(lines, mutedStyle.Render("  Target is unknown or internal storage; inspect SQL."))
	} else {
		lines = append(lines, "  "+d.Target.Table, "    "+partObservationText(*d.Target, d.Offline))
		lines = append(lines, fmt.Sprintf("  Visible MVs targeting this table: %d", len(d.Writers)))
		for _, writer := range d.Writers {
			lines = append(lines, "    "+writer)
		}
	}
	if d.PartError != "" {
		lines = append(lines, "", errorStyle.Render(d.PartError))
	}
	lines = append(lines, "", mutedStyle.Render("NewPart includes all writers; source/target events are not MV execution counts."), mutedStyle.Render("Logs may be disabled, sampled or delayed. No events do not prove inactivity."), mutedStyle.Render("[x] execution history · [r] refresh"))
	return lines
}
func partObservationText(o clickhouse.PartObservation, offline bool) string {
	if offline {
		return "part log unavailable offline"
	}
	switch o.State {
	case clickhouse.MetricAvailable:
		return fmt.Sprintf("NewPart: %d events · %d rows", o.Events, o.Rows)
	case clickhouse.MetricNoEvents:
		return "no NewPart events observed"
	case clickhouse.MetricUnsupported:
		return "unsupported engine for part metrics: " + o.Detail
	case clickhouse.MetricMetadataUnavailable:
		return "table metadata unavailable"
	case clickhouse.MetricLogError:
		return "part log unavailable: " + o.Detail
	default:
		return "part observations unavailable"
	}
}
