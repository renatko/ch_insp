package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ch_insp/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type nodesLoadedMsg struct {
	request uint64
	nodes   []clickhouse.Node
	err     error
}
type nodeSurveyLoadedMsg struct {
	request uint64
	survey  clickhouse.ClusterSurvey
}
type nodeConnectedMsg struct {
	request uint64
	dsn     string
	catalog clickhouse.Catalog
	err     error
}

func (m *model) loadNodes() tea.Cmd {
	m.nodeRequest++
	request := m.nodeRequest
	dsn := m.dsn
	if m.nodeCancel != nil {
		m.nodeCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.nodeCancel = cancel
	m.nodeLoading = true
	m.nodeError = nil
	m.nodeSurvey = nil
	m.nodeDetails = false
	return func() tea.Msg {
		defer cancel()
		nodes, err := clickhouse.LoadNodes(ctx, dsn)
		return nodesLoadedMsg{request: request, nodes: nodes, err: err}
	}
}
func (m *model) connectNode(node clickhouse.Node) tea.Cmd {
	dsn, err := clickhouse.NodeDSN(m.dsn, node)
	m.nodeRequest++
	request := m.nodeRequest
	if err != nil {
		m.nodeError = err
		return nil
	}
	if m.nodeCancel != nil {
		m.nodeCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.nodeCancel = cancel
	m.nodeLoading = true
	m.nodeError = nil
	return func() tea.Msg {
		defer cancel()
		catalog, err := clickhouse.LoadCatalog(ctx, dsn)
		err = clickhouse.RedactConnectionError(err, dsn)
		return nodeConnectedMsg{request: request, dsn: dsn, catalog: catalog, err: err}
	}
}
func (m *model) surveySelectedCluster() tea.Cmd {
	cluster := m.nodes[m.nodeCursor].Cluster
	dsn := m.dsn
	database := m.database
	nodes := append([]clickhouse.Node(nil), m.nodes...)
	if m.nodeCancel != nil {
		m.nodeCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	m.nodeCancel = cancel
	m.nodeRequest++
	request := m.nodeRequest
	m.nodeLoading = true
	m.nodeError = nil
	m.nodeDetails = false
	return func() tea.Msg {
		defer cancel()
		return nodeSurveyLoadedMsg{request: request, survey: clickhouse.SurveyClusterMetrics(ctx, dsn, nodes, cluster, database)}
	}
}
func (m model) selectedNodeSurvey() *clickhouse.NodeSurvey {
	if m.nodeSurvey == nil || len(m.nodes) == 0 || m.nodeCursor >= len(m.nodes) {
		return nil
	}
	node := m.nodes[m.nodeCursor]
	for i := range m.nodeSurvey.Nodes {
		if m.nodeSurvey.Nodes[i].Node == node {
			return &m.nodeSurvey.Nodes[i]
		}
	}
	return nil
}
func (m model) nodeListVisible() int {
	width := m.width
	if width <= 0 {
		width = 80
	}
	reserved := 13
	if m.nodeSurvey != nil {
		reserved = 12 + len(m.nodeSurveyLines(width))
	}
	if m.nodeError != nil {
		reserved++
	}
	return maxInt(1, m.height-reserved)
}

func (m model) nodeSurveyLines(width int) []string {
	if m.nodeSurvey == nil {
		return nil
	}
	survey := m.nodeSurvey
	suffix := ""
	if survey.Truncated {
		suffix = " · limited to 64"
	}
	responded := 0
	for _, result := range survey.Nodes {
		if result.Server != "" {
			responded++
		}
	}
	lines := []string{fmt.Sprintf("Survey %s: %d/%d nodes responded%s · e: details", survey.Cluster, responded, survey.Total, suffix)}
	if survey.Database != "" {
		var parts, reads, writes uint64
		partNodes, viewNodes := 0, 0
		seen := map[string]bool{}
		for _, result := range survey.Nodes {
			key := fmt.Sprintf("%s:%d", result.Node.Host, result.Node.Port)
			if seen[key] {
				continue
			}
			seen[key] = true
			if result.Error != "" {
				continue
			}
			if result.PartLogError == "" {
				parts += result.PartRows
				partNodes++
			}
			if result.ViewLogError == "" {
				reads += result.ViewReadRows
				writes += result.ViewWrittenRows
				viewNodes++
			}
		}
		lines = append(lines, fmt.Sprintf("%s · observed rows / 60s · node events, not logical totals", survey.Database))
		lines = append(lines, fmt.Sprintf("NewPart: %d (%d/%d nodes) · MV read: %d / write: %d (%d/%d nodes)", parts, partNodes, survey.Total, reads, writes, viewNodes, survey.Total))
		if partNodes != survey.Total || viewNodes != survey.Total {
			lines = append(lines, "Partial: missing, duplicate, or unchecked nodes/logs")
		}
	}
	var result []string
	for _, line := range lines {
		result = append(result, strings.Split(ansi.Hardwrap(line, maxInt(1, width), true), "\n")...)
	}
	return result
}
func (m model) renderNodes(width int) string {
	lines := []string{accentStyle.Render("SELECT NODE"), mutedStyle.Render("Current node: " + m.nodeName), mutedStyle.Render("Direct connections · v: survey selected cluster"), ""}
	if m.nodeDetails {
		details := m.selectedNodeSurvey()
		if details == nil {
			return strings.Join(append(lines, "No survey result.", "", mutedStyle.Render("[esc] back")), "\n")
		}
		all := m.nodeDetailLines(width)
		visible := maxInt(1, m.height-9)
		start := minInt(m.nodeDetailsOffset, maxInt(0, len(all)-visible))
		end := minInt(len(all), start+visible)
		lines = append(lines, all[start:end]...)
		return strings.Join(append(lines, "", mutedStyle.Render("↑/↓ or PgUp/PgDn scroll  [esc] back")), "\n")
	}
	if m.nodeLoading {
		lines = append(lines, mutedStyle.Render("Loading, connecting or surveying…"))
	} else if m.nodeError != nil {
		lines = append(lines, errorStyle.Render(m.nodeError.Error()))
	}
	if !m.nodeLoading {
		visible := m.nodeListVisible()
		start := maxInt(0, m.nodeCursor-visible+1)
		end := minInt(len(m.nodes), start+visible)
		if len(m.nodes) == 0 {
			lines = append(lines, mutedStyle.Render("No configured cluster nodes found."))
		}
		for i := start; i < end; i++ {
			n := m.nodes[i]
			line := fmt.Sprintf("  %s  shard %d replica %d  %s:%d", n.Cluster, n.Shard, n.Replica, n.Host, n.Port)
			if m.nodeSurvey != nil {
				for _, result := range m.nodeSurvey.Nodes {
					if result.Node == n {
						line += " · " + result.Summary()
						break
					}
				}
			}
			line = ansi.Truncate(line, maxInt(1, width), "…")
			if i == m.nodeCursor {
				line = activeStyle.Render("›" + strings.TrimPrefix(line, " "))
			}
			lines = append(lines, line)
		}
	}
	for _, line := range m.nodeSurveyLines(width) {
		if strings.HasPrefix(line, "Partial:") {
			lines = append(lines, errorStyle.Render(line))
		} else {
			lines = append(lines, mutedStyle.Render(line))
		}
	}
	return strings.Join(append(lines, "", mutedStyle.Render("↑/↓ select  [v] survey cluster  [e] details  [enter] connect  [esc] back")), "\n")
}
func detailOrOK(s string) string {
	if s == "" {
		return "ok"
	}
	return s
}

func (m model) nodeDetailLines(width int) []string {
	details := m.selectedNodeSurvey()
	if details == nil {
		return nil
	}
	part, view := detailOrOK(details.PartLogError), detailOrOK(details.ViewLogError)
	if details.Error != "" {
		part = "not checked"
		view = "not checked"
	}
	text := fmt.Sprintf("%s:%d · %s\nserver: %s\nversion: %s\nconnection: %s\npart_log: %s\nquery_views_log: %s", details.Node.Host, details.Node.Port, details.Node.Cluster, details.Server, details.Version, detailOrOK(details.Error), part, view)
	if m.nodeSurvey.Database != "" && details.Error == "" {
		if details.PartLogError == "" {
			text += fmt.Sprintf("\nNewPart rows / 60s: %d", details.PartRows)
		}
		if details.ViewLogError == "" {
			text += fmt.Sprintf("\nMV read / write rows / 60s: %d / %d", details.ViewReadRows, details.ViewWrittenRows)
		}
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, strings.Split(ansi.Hardwrap(line, maxInt(1, width), true), "\n")...)
	}
	return out
}
func (m model) nodeDetailMaxOffset() int {
	width := m.width
	if width <= 0 {
		width = 80
	}
	return maxInt(0, len(m.nodeDetailLines(width))-maxInt(1, m.height-9))
}
