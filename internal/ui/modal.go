package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Small terminals retain the full-screen layout so that controls remain usable.
func (m model) hasModal() bool {
	return !m.loading && m.err == nil && m.width >= 40 && m.height >= 20 &&
		(m.choosingNode || m.choosingDatabase || m.choosingDependency || m.searching || m.filtering || !m.overview)
}

func (m model) modalGeometry() (x, y, width, height int) {
	width, height = m.width-4, m.height-4
	if m.choosingNode || m.choosingDatabase || m.choosingDependency || m.searching || m.filtering {
		width = minInt(width, 84)
		height = minInt(height, 24)
	} else {
		width = m.width - 14
	}
	return (m.width - width) / 2, (m.height - height) / 2, width, height
}

func modalRow(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(s)))
}

func (m model) modalPadding() int {
	if m.filtering || m.searching {
		return 1
	}
	return 0
}

func (m model) breadcrumb() string {
	parts := []string{"Overview"}
	if !m.overview && len(m.views) > 0 {
		parts = append(parts, m.views[m.selected].Name)
	}
	switch {
	case m.choosingNode:
		parts = append(parts, "Nodes")
		if m.nodeDetails {
			parts = append(parts, "Details")
		}
	case m.choosingDatabase:
		parts = append(parts, "Database")
	case m.choosingDependency:
		parts = append(parts, "Choose table")
	case m.filtering:
		parts = append(parts, "Filters")
	case m.searching:
		parts = append(parts, "Search")
	case m.observationMode == "executions":
		parts = append(parts, "Executions")
	case m.observationMode == "diagnostics":
		parts = append(parts, "Diagnostics")
	case m.dependencies:
		if m.dependencyReturnScreen == "target" {
			parts = append(parts, "Target")
		}
		parts = append(parts, "Dependencies")
		parts = append(parts, m.dependencyHistory...)
		parts = append(parts, m.dependencyTable)
		if m.impact {
			parts = append(parts, "Impact")
			if m.impactColumn != "" {
				parts = append(parts, m.impactColumn)
			}
		}
	case m.target:
		if m.targetReturnDependencies {
			parts = append(parts, "Dependencies")
		}
		parts = append(parts, "Target")
		if m.targetData.Table != "" {
			parts = append(parts, m.targetData.Table)
		}
		if len(m.targetData.Columns) > 0 && m.targetColumn >= 0 && m.targetColumn < len(m.targetData.Columns) {
			parts = append(parts, m.targetData.Columns[m.targetColumn].Name)
		}
	case m.columns:
		parts = append(parts, "Columns")
	case m.definition:
		parts = append(parts, "SQL")
	case m.failuresMode:
		switch m.failuresReturnScreen {
		case "target":
			parts = append(parts, "Target")
		case "columns":
			parts = append(parts, "Columns")
		case "definition":
			parts = append(parts, "SQL")
		case "dependencies":
			parts = append(parts, "Dependencies")
		}
		parts = append(parts, "Errors")
		if m.failureDetail {
			parts = append(parts, "Message")
		}
	case !m.overview:
		parts = append(parts, "Flow")
	}
	return strings.Join(parts, " › ")
}

// Render the ordinary screen inside a framed viewport over the current overview.
// The same geometry is used to translate mouse events in Update.
func (m model) renderModal() string {
	x, y, width, height := m.modalGeometry()
	background := m
	background.modalInner, background.overview = true, true
	background.choosingNode, background.choosingDatabase, background.choosingDependency = false, false, false
	background.searching, background.filtering = false, false
	base := strings.Split(background.View(), "\n")
	inner := m
	padding := m.modalPadding()
	inner.modalInner, inner.width, inner.height = true, width-2-2*padding, height
	content := strings.Split(inner.View(), "\n")
	if len(content) >= 2 {
		content = content[2:]
	}
	innerHeight := height - 2
	if len(content) > innerHeight {
		content = append(content[:innerHeight-1], content[len(content)-1])
	}
	for len(content) < innerHeight {
		content = append(content, "")
	}
	frame := make([]string, height)
	path := " " + ansi.Truncate(m.breadcrumb(), width-6, "…") + " "
	frame[0] = mutedStyle.Render("╭─" + path + strings.Repeat("─", maxInt(0, width-3-lipgloss.Width(path))) + "╮")
	frame[height-1] = mutedStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")
	for i := 1; i < height-1; i++ {
		frame[i] = mutedStyle.Render("│") + strings.Repeat(" ", width-2) + mutedStyle.Render("│")
	}
	for i, line := range content {
		frame[i+1] = mutedStyle.Render("│") + strings.Repeat(" ", padding) + modalRow(line, width-2-2*padding) + strings.Repeat(" ", padding) + mutedStyle.Render("│")
	}
	for len(base) < m.height {
		base = append(base, "")
	}
	base = base[:m.height]
	// The overview's shortcuts are inactive while a panel has focus.
	base[len(base)-1] = ""
	for i := range base {
		base[i] = modalRow(base[i], m.width)
	}
	for i := 0; i < height; i++ {
		line := base[y+i]
		base[y+i] = ansi.Cut(line, 0, x) + frame[i] + ansi.Cut(line, x+width, m.width)
	}
	return strings.Join(base, "\n")
}
