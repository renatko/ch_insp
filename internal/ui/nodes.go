package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chinspector/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type nodesLoadedMsg struct {
	request uint64
	nodes   []clickhouse.Node
	err     error
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
	m.nodeLoading = true
	m.nodeError = nil
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	m.nodeLoading = true
	m.nodeError = nil
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		catalog, err := clickhouse.LoadCatalog(ctx, dsn)
		err = clickhouse.RedactConnectionError(err, dsn)
		return nodeConnectedMsg{request: request, dsn: dsn, catalog: catalog, err: err}
	}
}
func (m model) renderNodes(width int) string {
	lines := []string{accentStyle.Render("SELECT NODE"), mutedStyle.Render("Current node: " + m.nodeName), mutedStyle.Render("Connect directly using current credentials and TLS settings."), ""}
	if m.nodeLoading {
		lines = append(lines, mutedStyle.Render("Loading or connecting…"))
	} else if m.nodeError != nil {
		lines = append(lines, errorStyle.Render(m.nodeError.Error()))
	}
	if !m.nodeLoading {
		visible := maxInt(1, m.height-11)
		start := maxInt(0, m.nodeCursor-visible+1)
		end := minInt(len(m.nodes), start+visible)
		if len(m.nodes) == 0 {
			lines = append(lines, mutedStyle.Render("No configured cluster nodes found."))
		}
		for i := start; i < end; i++ {
			n := m.nodes[i]
			line := fmt.Sprintf("  %s  shard %d replica %d  %s:%d", n.Cluster, n.Shard, n.Replica, n.Host, n.Port)
			line = ansi.Truncate(line, maxInt(1, width), "…")
			if i == m.nodeCursor {
				line = activeStyle.Render("›" + strings.TrimPrefix(line, " "))
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(append(lines, "", mutedStyle.Render("↑/↓ select  [enter] connect  [esc] back")), "\n")
}
