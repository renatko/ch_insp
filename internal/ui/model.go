package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"chinspector/internal/clickhouse"
)

type viewsLoadedMsg struct {
	catalog clickhouse.Catalog
	err     error
}

type metricsTickMsg time.Time
type metricsLoadedMsg struct {
	key   string
	rates clickhouse.Rates
}
type overviewLoadedMsg struct {
	database string
	rates    clickhouse.OverviewRates
}

type model struct {
	dsn              string
	databases        []string
	database         string
	allViews         []clickhouse.View
	choosingDatabase bool
	databaseCursor   int
	views            []clickhouse.View
	selected         int
	loading          bool
	err              error
	width            int
	height           int
	definition       bool
	lineOffset       int
	columnOffset     int
	rates            clickhouse.Rates
	metricsBusy      bool
	metricsReady     bool
	overview         bool
	overviewBusy     bool
	overviewReady    bool
	overviewRates    clickhouse.OverviewRates
	overviewScroll   int
	overviewColumn   int
}

var (
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC")).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#0F172A")).Background(lipgloss.Color("#7DD3FC")).Bold(true)
	borderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#334155")).Padding(0, 1)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCA5A5"))
	sourceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#A5B4FC"))
	viewStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC"))
	targetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC"))
)

// Run starts the interactive materialized view browser.
func Run(dsn string) error {
	_, err := tea.NewProgram(model{dsn: dsn, loading: true}, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadViews(m.dsn), metricsTick())
}

func metricsTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return metricsTickMsg(t) })
}

func loadMetrics(dsn string, view clickhouse.View) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return metricsLoadedMsg{key: view.Database + "\x00" + view.Name, rates: clickhouse.LoadRates(ctx, dsn, view)}
	}
}

func loadOverview(dsn, database string, views []clickhouse.View) tea.Cmd {
	views = append([]clickhouse.View(nil), views...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return overviewLoadedMsg{database: database, rates: clickhouse.LoadOverviewRates(ctx, dsn, views)}
	}
}

func loadViews(dsn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		catalog, err := clickhouse.LoadCatalog(ctx, dsn)
		return viewsLoadedMsg{catalog: catalog, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case viewsLoadedMsg:
		m.loading, m.err = false, msg.err
		m.metricsReady = false
		m.overviewReady = false
		if msg.err == nil {
			m.databases, m.allViews = msg.catalog.Databases, msg.catalog.Views
			if !contains(m.databases, m.database) {
				m.database = msg.catalog.CurrentDatabase
				if !contains(m.databases, m.database) {
					m.database = ""
					if len(m.databases) > 0 {
						m.database = m.databases[0]
					}
				}
			}
			m.filterViews()
		}
		if msg.err == nil && m.overview && len(m.views) > 0 && !m.overviewBusy {
			m.overviewBusy = true
			return m, loadOverview(m.dsn, m.database, m.views)
		}
		if msg.err == nil && !m.overview && len(m.views) > 0 && !m.metricsBusy {
			m.metricsBusy = true
			return m, loadMetrics(m.dsn, m.views[m.selected])
		}
	case metricsTickMsg:
		if m.overview && len(m.views) > 0 && !m.loading && m.err == nil && !m.overviewBusy {
			m.overviewBusy = true
			return m, tea.Batch(metricsTick(), loadOverview(m.dsn, m.database, m.views))
		}
		if !m.overview && len(m.views) > 0 && !m.loading && m.err == nil && !m.metricsBusy {
			m.metricsBusy = true
			return m, tea.Batch(metricsTick(), loadMetrics(m.dsn, m.views[m.selected]))
		}
		return m, metricsTick()
	case metricsLoadedMsg:
		m.metricsBusy = false
		if len(m.views) > 0 && msg.key == m.views[m.selected].Database+"\x00"+m.views[m.selected].Name {
			m.rates, m.metricsReady = msg.rates, true
		}
	case overviewLoadedMsg:
		m.overviewBusy = false
		if msg.database == m.database {
			m.overviewRates, m.overviewReady = msg.rates, true
		}
	case tea.KeyMsg:
		if m.choosingDatabase {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc", "b":
				m.choosingDatabase = false
			case "up", "k":
				m.databaseCursor = maxInt(0, m.databaseCursor-1)
			case "down", "j":
				m.databaseCursor = minInt(maxInt(0, len(m.databases)-1), m.databaseCursor+1)
			case "enter":
				if len(m.databases) > 0 {
					m.database = m.databases[m.databaseCursor]
					m.selected, m.lineOffset, m.columnOffset = 0, 0, 0
					m.metricsReady = false
					m.overviewReady = false
					m.overviewScroll, m.overviewColumn = 0, 0
					m.filterViews()
				}
				m.choosingDatabase = false
				if m.overview && len(m.views) > 0 && !m.overviewBusy {
					m.overviewBusy = true
					return m, loadOverview(m.dsn, m.database, m.views)
				}
				if !m.overview && len(m.views) > 0 && !m.metricsBusy {
					m.metricsBusy = true
					return m, loadMetrics(m.dsn, m.views[m.selected])
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading, m.err = true, nil
			m.lineOffset, m.columnOffset = 0, 0
			return m, loadViews(m.dsn)
		case "o":
			m.overview = !m.overview
			m.overviewScroll, m.overviewColumn = 0, 0
			if m.overview && len(m.views) > 0 && !m.overviewBusy {
				m.overviewBusy = true
				return m, loadOverview(m.dsn, m.database, m.views)
			}
		case "enter":
			if m.overview && len(m.views) > 0 {
				m.overview = false
				m.lineOffset, m.columnOffset = 0, 0
				m.metricsReady = false
				if !m.metricsBusy {
					m.metricsBusy = true
					return m, loadMetrics(m.dsn, m.views[m.selected])
				}
			}
		case "b":
			if !m.loading && m.err == nil {
				m.choosingDatabase = true
				m.databaseCursor = 0
				for i, name := range m.databases {
					if name == m.database {
						m.databaseCursor = i
						break
					}
				}
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.lineOffset = 0
				m.columnOffset = 0
				m.metricsReady = false
				if m.overview {
					m.keepOverviewSelectionVisible()
				}
			}
		case "down", "j":
			if m.selected < len(m.views)-1 {
				m.selected++
				m.lineOffset = 0
				m.columnOffset = 0
				m.metricsReady = false
				if m.overview {
					m.keepOverviewSelectionVisible()
				}
			}
		case "d":
			if m.overview {
				break
			}
			m.definition = !m.definition
			m.lineOffset = 0
			m.columnOffset = 0
		case "pgdown":
			if m.overview {
				lines, _ := m.overviewLines()
				m.overviewScroll = minInt(m.overviewScroll+maxInt(1, m.height-7), maxInt(0, len(lines)-maxInt(1, m.height-7)))
				break
			}
			if len(m.views) > 0 {
				m.lineOffset = minInt(m.lineOffset+maxInt(1, m.height-10), maxInt(0, len(m.detailLines())-maxInt(1, m.height-10)))
			}
		case "pgup":
			if m.overview {
				m.overviewScroll = maxInt(0, m.overviewScroll-maxInt(1, m.height-7))
				break
			}
			m.lineOffset = maxInt(0, m.lineOffset-maxInt(1, m.height-10))
		case "right", "l":
			if m.overview {
				lines, _ := m.overviewLines()
				longest := 0
				for _, line := range lines {
					longest = maxInt(longest, lipgloss.Width(line))
				}
				m.overviewColumn = minInt(m.overviewColumn+8, maxInt(0, longest-maxInt(1, m.width)))
				break
			}
			if len(m.views) > 0 {
				longest := 0
				for _, line := range m.detailLines() {
					longest = maxInt(longest, lipgloss.Width(line))
				}
				detailWidth := m.detailWidth()
				m.columnOffset = minInt(m.columnOffset+8, maxInt(0, longest-detailWidth))
			}
		case "left", "h":
			if m.overview {
				m.overviewColumn = maxInt(0, m.overviewColumn-8)
				break
			}
			m.columnOffset = maxInt(0, m.columnOffset-8)
		}
	}
	return m, nil
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (m *model) filterViews() {
	m.views = nil
	for _, view := range m.allViews {
		if view.Database == m.database {
			m.views = append(m.views, view)
		}
	}
	if m.selected >= len(m.views) {
		m.selected = maxInt(0, len(m.views)-1)
	}
}

func (m model) detailWidth() int {
	width := m.width
	if width <= 0 {
		width = 80
	}
	if width < 70 {
		return maxInt(1, maxInt(8, width-4)-4)
	}
	return maxInt(28, width-maxInt(26, width/3)-5) - 4
}

func (m model) View() string {
	var b strings.Builder
	width := m.width
	if width <= 0 {
		width = 80
	}

	b.WriteString(accentStyle.Render("CHINSPECTOR"))
	b.WriteString(mutedStyle.Render("  /  ClickHouse materialized views"))
	if m.database != "" {
		b.WriteString(accentStyle.Render("  /  DB: " + m.database))
	}
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(mutedStyle.Render("Connecting and loading materialized views…"))
		return b.String()
	}
	if m.err != nil {
		b.WriteString(errorStyle.Render("Could not load materialized views"))
		b.WriteString("\n")
		b.WriteString(m.err.Error())
		b.WriteString("\n\n")
		b.WriteString(mutedStyle.Render("Set CLICKHOUSE_URL, then press r to retry. Press q to quit."))
		return b.String()
	}
	if m.choosingDatabase {
		b.WriteString(accentStyle.Render("SELECT DATABASE"))
		b.WriteString("\n\n")
		if len(m.databases) == 0 {
			b.WriteString(mutedStyle.Render("No databases available."))
		}
		limit := maxInt(1, m.height-8)
		start := maxInt(0, m.databaseCursor-limit+1)
		end := minInt(len(m.databases), start+limit)
		for i := start; i < end; i++ {
			name := ansi.Truncate(m.databases[i], maxInt(1, width-4), "…")
			if i == m.databaseCursor {
				b.WriteString(activeStyle.Render("› " + name))
			} else {
				b.WriteString("  " + name)
			}
			if m.databases[i] == m.database {
				b.WriteString(mutedStyle.Render("  active"))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n" + mutedStyle.Render("↑/↓ select  Enter switch  Esc cancel  q quit"))
		return b.String()
	}
	if m.overview {
		return b.String() + m.renderOverview(width)
	}
	if width < 70 {
		if len(m.views) > 0 {
			b.WriteString(mutedStyle.Render(ansi.Truncate("SELECTED: "+m.views[m.selected].Name, width, "…")))
			b.WriteString("\n")
		}
		height := maxInt(5, m.height-9)
		panelWidth := maxInt(8, width-4)
		b.WriteString(borderStyle.Width(panelWidth).Height(height).Render(m.renderDetail(maxInt(1, panelWidth-4), height)))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(ansi.Truncate("o overview  b databases  ↑/↓ select  ←/→ scroll  d SQL  r  q", width, "…")))
		return b.String()
	}

	listWidth := maxInt(26, width/3)
	detailWidth := width - listWidth - 5
	if detailWidth < 28 {
		detailWidth = 28
	}
	height := maxInt(5, m.height-8)
	list := m.renderList(listWidth-4, height)
	detail := m.renderDetail(detailWidth-4, height)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		borderStyle.Width(listWidth).Height(height).Render(list),
		" ",
		borderStyle.Width(detailWidth).Height(height).Render(detail),
	))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("o overview  b databases  ↑/↓ select  ←/→ scroll  PgUp/PgDn scroll  d SQL  r refresh  q quit"))
	return b.String()
}

func (m model) renderList(width, height int) string {
	var b strings.Builder
	b.WriteString(accentStyle.Render("MATERIALIZED VIEWS"))
	b.WriteString("\n")
	if len(m.views) == 0 {
		b.WriteString(mutedStyle.Render("No materialized views found."))
		return b.String()
	}
	limit := maxInt(1, height-2)
	start := maxInt(0, m.selected-limit+1)
	end := start + limit
	if end > len(m.views) {
		end = len(m.views)
	}
	for i := start; i < end; i++ {
		view := m.views[i]
		label := view.Name
		label = truncate(label, width-2)
		if i == m.selected {
			b.WriteString(activeStyle.Width(width - 1).Render("› " + label))
		} else {
			b.WriteString("  " + label)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// overviewLines renders every view's flow with a stable selection position.
func (m model) overviewLines() ([]string, []int) {
	lines := []string{accentStyle.Render("FLOW OVERVIEW"), mutedStyle.Render("SQL-inferred inputs · local log averages over 60s · refresh ~5s"), mutedStyle.Render("Repeated table names connect diagrams."), ""}
	starts := make([]int, len(m.views))
	if len(m.views) == 0 {
		return append(lines, mutedStyle.Render("No materialized views in this database.")), starts
	}
	for i, view := range m.views {
		starts[i] = len(lines)
		marker := "  "
		if i == m.selected {
			marker = accentStyle.Render("› ")
		}
		lines = append(lines, marker+accentStyle.Render(view.Name))
		rates := clickhouse.Rates{Tables: m.overviewRates.Tables}
		if rate, ok := m.overviewRates.Views[view.Database+"."+view.Name]; ok {
			rates.MVAvailable, rates.MVRead, rates.MVWritten = true, rate.Read, rate.Written
		}
		for _, line := range diagramLines(view, rates, m.overviewReady) {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	}
	lines = append(lines, mutedStyle.Render("JOIN inputs may not trigger incremental MVs."), mutedStyle.Render("No data may mean no events, delayed logs, or logging disabled."))
	if m.overviewReady {
		for _, err := range m.overviewRates.Errors {
			lines = append(lines, errorStyle.Render("Metrics: "+err))
		}
	}
	return lines, starts
}

func (m *model) keepOverviewSelectionVisible() {
	_, starts := m.overviewLines()
	if m.selected >= len(starts) {
		return
	}
	visible := maxInt(1, m.height-7)
	line := starts[m.selected]
	if line < m.overviewScroll {
		m.overviewScroll = line
	} else if line+5 >= m.overviewScroll+visible {
		m.overviewScroll = line
	}
}

func (m model) renderOverview(width int) string {
	lines, _ := m.overviewLines()
	visible := maxInt(1, m.height-7)
	start := minInt(m.overviewScroll, maxInt(0, len(lines)-visible))
	end := minInt(len(lines), start+visible)
	for i := start; i < end; i++ {
		lines[i] = ansi.Cut(lines[i], m.overviewColumn, m.overviewColumn+maxInt(1, width))
	}
	footer := "o back  Enter open MV  ↑/↓ select  ←/→ scroll  PgUp/PgDn  b database  r refresh  q quit"
	return strings.Join(lines[start:end], "\n") + "\n\n" + mutedStyle.Render(ansi.Truncate(footer, width, "…"))
}

func (m model) renderDetail(width, height int) string {
	if len(m.views) == 0 {
		return mutedStyle.Render("Select a materialized view to inspect its definition.")
	}
	lines := m.detailLines()
	visible := maxInt(1, height-2)
	start := minInt(m.lineOffset, maxInt(0, len(lines)-visible))
	end := minInt(start+visible, len(lines))
	for i := start; i < end; i++ {
		lines[i] = ansi.Cut(strings.TrimRight(lines[i], "\r"), m.columnOffset, m.columnOffset+width)
	}
	return strings.Join(lines[start:end], "\n")
}

func (m model) detailLines() []string {
	view := m.views[m.selected]
	var lines []string
	if m.definition {
		lines = append(lines, accentStyle.Render("CREATE DEFINITION"), "")
		lines = append(lines, strings.Split(view.Definition, "\n")...)
	} else {
		lines = append(lines, accentStyle.Render("DATA FLOW"), mutedStyle.Render("Inferred from SQL · arrows show query inputs"), "")
		lines = append(lines, diagramLines(view, m.rates, m.metricsReady)...)
		lines = append(lines, "", mutedStyle.Render("Local logs: last 60s average; refresh ~5s; may be delayed."), mutedStyle.Render("No data may mean no events or logging disabled."), mutedStyle.Render("JOIN inputs may not trigger incremental MVs."))
		if m.metricsReady {
			for _, err := range m.rates.Errors {
				lines = append(lines, errorStyle.Render("Metrics: "+err))
			}
		}
		lines = append(lines, mutedStyle.Render("d: SQL definition · ←/→: scroll diagram"))
	}
	return lines
}

func diagramLines(view clickhouse.View, rates clickhouse.Rates, ready bool) []string {
	flow := clickhouse.InferFlow(view)
	sources := flow.Sources
	if len(sources) == 0 {
		sources = []string{"? unknown source"}
	}
	rate := func(name string) string {
		if !ready {
			return "parts: loading"
		}
		v, ok := rates.Tables[name]
		if !ok {
			return "parts: no data"
		}
		return fmt.Sprintf("parts: %.1f rows/s", v)
	}
	sourceWidth := len("QUERY INPUT")
	sourceColumnWidth := 0
	for _, source := range sources {
		sourceWidth = maxInt(sourceWidth, lipgloss.Width(shortName(source)))
		sourceColumnWidth = maxInt(sourceColumnWidth, lipgloss.Width(rate(source)))
	}
	sourceColumnWidth = maxInt(sourceColumnWidth, sourceWidth+4)
	mvMetric := "read/write: no data"
	if !ready {
		mvMetric = "read/write: loading"
	} else if rates.MVAvailable {
		mvMetric = fmt.Sprintf("read %.1f / write %.1f rows/s", rates.MVRead, rates.MVWritten)
	}
	mv := diagramBox("MATERIALIZED VIEW", view.Name, mvMetric, 0, 0, true, viewStyle)
	targetMV := "MV: no data"
	targetTotal := "total: no data"
	if !ready {
		targetMV, targetTotal = "MV: loading", "total: loading"
	} else {
		if rates.MVAvailable {
			targetMV = fmt.Sprintf("MV: %.1f rows/s", rates.MVWritten)
		}
		if value, ok := rates.Tables[flow.Target]; ok {
			targetTotal = fmt.Sprintf("total: %.1f rows/s", value)
		}
	}
	target := diagramBox("TARGET", shortName(flow.Target), targetMV+" · "+targetTotal, 0, 0, false, targetStyle)
	total := len(sources) * 5
	middle := (len(sources)/2)*5 + 2
	top := middle - 2
	lines := make([]string, total)
	for row := 0; row < total; row++ {
		box := diagramBox("QUERY INPUT", shortName(sources[row/5]), rate(sources[row/5]), sourceWidth, sourceColumnWidth, true, sourceStyle)
		branch := "     "
		if len(sources) == 1 && row == middle {
			branch = "────▶"
		} else if len(sources) > 1 {
			first, last := 2, (len(sources)-1)*5+2
			switch {
			case row == middle && row == last:
				branch = "──┴─▶"
			case row == middle && row == first:
				branch = "──┬─▶"
			case row == middle:
				branch = "──┼─▶"
			case row == first:
				branch = "──┐  "
			case row == last:
				branch = "──┘  "
			case row > first && row < last && row%5 == 2:
				branch = "──┤  "
			case row > first && row < last:
				branch = "  │  "
			}
		}
		line := box[row%5] + mutedStyle.Render(branch)
		if row >= top && row < top+5 {
			connector := "     "
			if row == middle {
				connector = "────▶"
			}
			line += mv[row-top] + mutedStyle.Render(connector) + target[row-top]
		}
		lines[row] = line
	}
	return lines
}

func shortName(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func diagramBox(label, name, metric string, width, columnWidth int, outgoing bool, style lipgloss.Style) []string {
	width = maxInt(width, maxInt(lipgloss.Width(label), lipgloss.Width(name)))
	columnWidth = maxInt(columnWidth, maxInt(width+4, lipgloss.Width(metric)))
	pad := func(s string) string { return s + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(s))) }
	gap := columnWidth - width - 4
	lineGap := strings.Repeat(" ", gap)
	if outgoing {
		lineGap = strings.Repeat("─", gap)
	}
	return []string{
		style.Render("╭─"+strings.Repeat("─", width)+"─╮") + strings.Repeat(" ", gap),
		style.Render("│ "+pad(label)+" │") + strings.Repeat(" ", gap),
		style.Bold(true).Render("│ "+pad(name)+" │") + mutedStyle.Render(lineGap),
		style.Render("╰─"+strings.Repeat("─", width)+"─╯") + strings.Repeat(" ", gap),
		mutedStyle.Render(metric) + strings.Repeat(" ", maxInt(0, columnWidth-lipgloss.Width(metric))),
	}
}

func truncate(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(value) + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
