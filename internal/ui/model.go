package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ch_insp/internal/clickhouse"
)

type viewsLoadedMsg struct {
	catalog clickhouse.Catalog
	dump    *clickhouse.SQLDump
	err     error
}

type metricsTickMsg time.Time
type metricsLoadedMsg struct {
	epoch  uint64
	period int
	key    string
	rates  clickhouse.Rates
}
type overviewLoadedMsg struct {
	epoch    uint64
	period   int
	database string
	revision int
	rates    clickhouse.OverviewRates
}
type columnsLoadedMsg struct {
	key      string
	revision int
	request  uint64
	data     clickhouse.ColumnLineage
	err      error
}

type impactLoadedMsg struct {
	request  uint64
	revision int
	result   clickhouse.ImpactResult
}

type model struct {
	modalInner                     bool
	dsn                            string
	schemaPath                     string
	schemaDatabase                 string
	dump                           *clickhouse.SQLDump
	databases                      []string
	database                       string
	nodeName                       string
	nodeVersion                    string
	choosingNode                   bool
	nodeLoading                    bool
	nodeError                      error
	nodes                          []clickhouse.Node
	nodeCursor                     int
	nodeRequest                    uint64
	nodeCancel                     context.CancelFunc
	nodeSurvey                     *clickhouse.ClusterSurvey
	nodeDetails                    bool
	nodeDetailsOffset              int
	allViews                       []clickhouse.View
	choosingDatabase               bool
	databaseCursor                 int
	filtering                      bool
	searching                      bool
	searchQuery                    string
	searchCursor                   int
	filterSource                   []string
	filterTarget                   []string
	draftSource                    []string
	draftTarget                    []string
	filterField                    int
	filterCursors                  [2]int
	filterQuery                    [2]string
	filterEditing                  bool
	viewRevision                   int
	views                          []clickhouse.View
	selected                       int
	loading                        bool
	err                            error
	width                          int
	height                         int
	definition                     bool
	sqlOriginal                    bool
	dependencies                   bool
	dependencyTable                string
	choosingDependency             bool
	dependencyChoices              []string
	dependencyChoice               int
	dependencyCursor               int
	dependencyHistory              []string
	dependencyReturnTarget         bool
	dependencyReturnScreen         string
	dependencyReturnTargetExplicit string
	dependencyMessage              string
	impact                         bool
	impactBackTarget               bool
	impactLoading                  bool
	impactColumn                   string
	impactResult                   clickhouse.ImpactResult
	impactFilter                   string
	impactFiltering                bool
	observationMode                string
	observationReturnOverview      bool
	observationLoading             bool
	observationError               error
	observationExecutions          []clickhouse.ViewExecution
	observationDiagnostics         clickhouse.FlowDiagnostics
	observationSelected            int
	observationDetail              bool
	observationWindow              int
	failuresMode                   bool
	failuresReturnScreen           string
	failuresLoading                bool
	failuresError                  error
	failures                       []clickhouse.ViewFailure
	failureSelected                int
	failureDetail                  bool
	columns                        bool
	target                         bool
	targetReturnOverview           bool
	targetReturnDependencies       bool
	targetExplicit                 string
	targetColumn                   int
	targetLoading                  bool
	targetData                     clickhouse.TargetColumns
	targetError                    error
	columnsLoading                 bool
	columnsData                    clickhouse.ColumnLineage
	columnsError                   error
	columnsCancel                  context.CancelFunc
	columnsRequest                 uint64
	lineOffset                     int
	columnOffset                   int
	rates                          clickhouse.Rates
	metricsBusy                    bool
	metricsReady                   bool
	overview                       bool
	chartPeriod                    int
	statusWindow                   int
	chartCommon                    bool
	chartEpoch                     uint64
	overviewTargetFocus            bool
	overviewBusy                   bool
	overviewReady                  bool
	overviewRates                  clickhouse.OverviewRates
	overviewScroll                 int
	overviewColumn                 int
}

var (
	accentStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC")).Bold(true)
	mutedStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	activeStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#0F172A")).Background(lipgloss.Color("#7DD3FC")).Bold(true)
	borderStyle         = lipgloss.NewStyle().Border(lipgloss.Border{Top: " ", Bottom: " ", Left: " ", Right: " ", TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " "}).Padding(0, 1)
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCA5A5"))
	sourceStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#A5B4FC"))
	viewStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC"))
	targetStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC"))
	readMetricStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6BA9BF"))
	writeMetricStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#BBA269"))
	selectionBackground = lipgloss.Color("#334155")
)

// Run starts the interactive materialized view browser.
func Run(dsn string) error {
	_, err := tea.NewProgram(newModel(dsn, "", ""), tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

func RunSchema(path, database string) error {
	_, err := tea.NewProgram(newModel("offline:", path, database), tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

func newModel(dsn, schemaPath, schemaDatabase string) model {
	return model{dsn: dsn, schemaPath: schemaPath, schemaDatabase: schemaDatabase, loading: true, overview: true}
}

func loadSchema(path, database string) tea.Cmd {
	return func() tea.Msg {
		dump, err := clickhouse.LoadSQLDump(path, database)
		if err != nil {
			return viewsLoadedMsg{err: err}
		}
		return viewsLoadedMsg{catalog: dump.Catalog, dump: dump}
	}
}

func (m model) Init() tea.Cmd {
	if m.schemaPath != "" {
		return loadSchema(m.schemaPath, m.schemaDatabase)
	}
	return tea.Batch(loadViews(m.dsn), metricsTick())
}

func metricsTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return metricsTickMsg(t) })
}

func loadMetrics(dsn string, view clickhouse.View, period int, hours int, epoch uint64) tea.Cmd {
	return func() tea.Msg {
		if dsn == "offline:" {
			all := offlineOverview([]clickhouse.View{view})
			return metricsLoadedMsg{epoch: epoch, period: period, key: view.Database + "\x00" + view.Name, rates: clickhouse.Rates{Tables: all.Tables, TableDiagnostics: all.TableDiagnostics, MVDiagnostic: clickhouse.MetricDiagnostic{State: clickhouse.MetricOffline}}}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return metricsLoadedMsg{epoch: epoch, period: period, key: view.Database + "\x00" + view.Name, rates: clickhouse.LoadRates(ctx, dsn, view, period, hours)}
	}
}

func loadOverview(dsn, database string, views []clickhouse.View, revision int, period int, hours int, epoch uint64) tea.Cmd {
	views = append([]clickhouse.View(nil), views...)
	return func() tea.Msg {
		if dsn == "offline:" {
			return overviewLoadedMsg{epoch: epoch, period: period, database: database, revision: revision, rates: offlineOverview(views)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return overviewLoadedMsg{epoch: epoch, period: period, database: database, revision: revision, rates: clickhouse.LoadOverviewRates(ctx, dsn, views, period, hours)}
	}
}

func offlineOverview(views []clickhouse.View) clickhouse.OverviewRates {
	result := clickhouse.OverviewRates{Tables: map[string]float64{}, Views: map[string]clickhouse.ViewRate{}, TableDiagnostics: map[string]clickhouse.MetricDiagnostic{}, ViewDiagnostics: map[string]clickhouse.MetricDiagnostic{}}
	for _, view := range views {
		result.ViewDiagnostics[view.Database+"."+view.Name] = clickhouse.MetricDiagnostic{State: clickhouse.MetricOffline}
		flow := clickhouse.InferFlow(view)
		for _, name := range append(flow.Sources, flow.Target) {
			result.TableDiagnostics[name] = clickhouse.MetricDiagnostic{State: clickhouse.MetricOffline}
		}
	}
	return result
}

func loadViews(dsn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		catalog, err := clickhouse.LoadCatalog(ctx, dsn)
		return viewsLoadedMsg{catalog: catalog, err: err}
	}
}

func (m *model) loadColumns() tea.Cmd {
	if len(m.views) == 0 {
		return nil
	}
	m.cancelColumns()
	view, revision, dsn := m.views[m.selected], m.viewRevision, m.dsn
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.columnsCancel = cancel
	m.columnsRequest++
	request := m.columnsRequest
	dump := m.dump
	m.columnsLoading, m.columnsError = true, nil
	return func() tea.Msg {
		defer cancel()
		var data clickhouse.ColumnLineage
		var err error
		if dump != nil {
			data, err = dump.ColumnLineage(view)
		} else {
			data, err = clickhouse.LoadColumnLineage(ctx, dsn, view)
		}
		return columnsLoadedMsg{key: view.Database + "\x00" + view.Name, revision: revision, request: request, data: data, err: err}
	}
}

func (m *model) cancelColumns() {
	if m.columnsCancel != nil {
		m.columnsCancel()
		m.columnsCancel = nil
	}
	m.columnsLoading = false
	m.targetLoading = false
	m.failuresLoading = false
	m.observationLoading = false
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.modalInner {
		if _, resizing := msg.(tea.WindowSizeMsg); !resizing && m.hasModal() {
			x, y, width, height := m.modalGeometry()
			padding := m.modalPadding()
			inner := m
			inner.modalInner, inner.width, inner.height = true, width-2-2*padding, height
			if mouse, ok := msg.(tea.MouseMsg); ok {
				if mouse.X <= x+padding || mouse.X >= x+width-1-padding || mouse.Y <= y || mouse.Y >= y+height-1 {
					return m, nil
				}
				mouse.X -= x + 1 + padding
				mouse.Y = mouse.Y - y + 1
				msg = mouse
			}
			next, cmd := inner.Update(msg)
			updated := next.(model)
			updated.modalInner, updated.width, updated.height = false, m.width, m.height
			return updated, cmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case viewsLoadedMsg:
		m.loading, m.err = false, msg.err
		m.metricsReady = false
		m.overviewReady = false
		if msg.err == nil {
			if m.schemaPath != "" {
				m.dump = msg.dump
			}
			m.databases, m.allViews = msg.catalog.Databases, msg.catalog.Views
			m.nodeName = msg.catalog.Node
			m.nodeVersion = msg.catalog.ServerVersion
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
			return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
		}
		if msg.err == nil && !m.overview && (m.columns || m.target || m.failuresMode) && len(m.views) > 0 {
			cmd := m.columnCmdIfActive()
			if !m.metricsBusy {
				m.metricsBusy = true
				return m, tea.Batch(cmd, loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch))
			}
			return m, cmd
		}
		if msg.err == nil && !m.overview && len(m.views) > 0 && !m.metricsBusy {
			m.metricsBusy = true
			return m, loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
		}
	case nodesLoadedMsg:
		if m.choosingNode && msg.request == m.nodeRequest {
			m.nodeLoading = false
			m.nodeCancel = nil
			m.nodes = msg.nodes
			m.nodeError = msg.err
			m.nodeCursor = 0
		}
	case nodeSurveyLoadedMsg:
		if m.choosingNode && msg.request == m.nodeRequest {
			m.nodeLoading = false
			m.nodeCancel = nil
			m.nodeSurvey = &msg.survey
			m.nodeError = nil
		}
	case nodeConnectedMsg:
		if m.choosingNode && msg.request == m.nodeRequest {
			m.nodeLoading = false
			m.nodeCancel = nil
			if msg.err != nil {
				m.nodeError = msg.err
				return m, nil
			}
			m.cancelColumns()
			m.dsn = msg.dsn
			m.nodeName = msg.catalog.Node
			m.nodeVersion = msg.catalog.ServerVersion
			m.databases = msg.catalog.Databases
			m.allViews = msg.catalog.Views
			if !contains(m.databases, m.database) {
				m.database = msg.catalog.CurrentDatabase
			}
			m.filterViews()
			m.selected = 0
			m.lineOffset = 0
			m.columnOffset = 0
			m.overviewScroll = 0
			m.overviewColumn = 0
			m.chartEpoch++
			m.metricsBusy = false
			m.overviewBusy = false
			m.metricsReady = false
			m.overviewReady = false
			m.choosingNode = false
			if len(m.views) > 0 {
				if m.overview {
					m.overviewBusy = true
					return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
				}
				m.metricsBusy = true
				return m, tea.Batch(loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch), m.columnCmdIfActive())
			}
		}
	case metricsTickMsg:
		if m.schemaPath != "" {
			return m, nil
		}
		if m.overview && len(m.views) > 0 && !m.loading && m.err == nil && !m.overviewBusy {
			m.overviewBusy = true
			return m, tea.Batch(metricsTick(), loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch))
		}
		if !m.overview && len(m.views) > 0 && !m.loading && m.err == nil && !m.metricsBusy {
			m.metricsBusy = true
			return m, tea.Batch(metricsTick(), loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch))
		}
		return m, metricsTick()
	case metricsLoadedMsg:
		if msg.epoch != m.chartEpoch || (msg.period != 0 && msg.period != m.chartPeriodSeconds()) {
			return m, nil
		}
		m.metricsBusy = false
		if len(m.views) > 0 && msg.key == m.views[m.selected].Database+"\x00"+m.views[m.selected].Name {
			m.rates, m.metricsReady = msg.rates, true
		}
	case overviewLoadedMsg:
		if msg.epoch != m.chartEpoch || (msg.period != 0 && msg.period != m.chartPeriodSeconds()) {
			return m, nil
		}
		m.overviewBusy = false
		if msg.database == m.database && msg.revision == m.viewRevision {
			m.overviewRates, m.overviewReady = msg.rates, true
		}
	case failuresLoadedMsg:
		if m.failuresMode && !m.overview && len(m.views) > 0 && msg.request == m.columnsRequest && msg.revision == m.viewRevision && msg.key == m.views[m.selected].Database+"\x00"+m.views[m.selected].Name {
			m.failuresLoading, m.failures, m.failuresError = false, msg.failures, msg.err
			m.columnsCancel = nil
		}
	case targetLoadedMsg:
		if m.target && !m.overview && len(m.views) > 0 && msg.request == m.columnsRequest && msg.revision == m.viewRevision && msg.key == m.views[m.selected].Database+"\x00"+m.views[m.selected].Name {
			m.targetLoading, m.targetData, m.targetError = false, msg.data, msg.err
			m.columnsCancel = nil
		}
	case columnsLoadedMsg:
		if m.columns && !m.overview && len(m.views) > 0 && msg.request == m.columnsRequest && msg.revision == m.viewRevision && msg.key == m.views[m.selected].Database+"\x00"+m.views[m.selected].Name {
			m.columnsLoading, m.columnsData, m.columnsError = false, msg.data, msg.err
			m.columnsCancel = nil
		}
	case impactLoadedMsg:
		if m.impact && msg.request == m.columnsRequest && msg.revision == m.viewRevision {
			m.impactLoading, m.impactResult = false, msg.result
			m.columnsCancel = nil
		}
	case observationLoadedMsg:
		if m.observationMode == msg.mode && msg.request == m.columnsRequest && msg.revision == m.viewRevision && len(m.views) > 0 && msg.key == m.views[m.selected].Database+"\x00"+m.views[m.selected].Name {
			m.observationLoading, m.observationError = false, msg.err
			m.observationExecutions, m.observationDiagnostics = msg.executions, msg.diagnostics
			m.columnsCancel = nil
		}
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyMsg:
		if m.choosingNode {
			switch msg.String() {
			case "esc":
				if m.nodeDetails {
					m.nodeDetails = false
					m.nodeDetailsOffset = 0
					return m, nil
				}
				if m.nodeCancel != nil {
					m.nodeCancel()
					m.nodeCancel = nil
				}
				m.choosingNode = false
				m.nodeRequest++
			case "q", "ctrl+c":
				if m.nodeCancel != nil {
					m.nodeCancel()
				}
				return m, tea.Quit
			case "up", "k":
				if m.nodeDetails {
					m.nodeDetailsOffset = maxInt(0, m.nodeDetailsOffset-1)
					break
				}
				m.nodeCursor = maxInt(0, m.nodeCursor-1)
			case "down", "j":
				if m.nodeDetails {
					m.nodeDetailsOffset = minInt(m.nodeDetailMaxOffset(), m.nodeDetailsOffset+1)
					break
				}
				m.nodeCursor = minInt(maxInt(0, len(m.nodes)-1), m.nodeCursor+1)
			case "v":
				if !m.nodeLoading && len(m.nodes) > 0 {
					return m, m.surveySelectedCluster()
				}
			case "e":
				if !m.nodeLoading && m.selectedNodeSurvey() != nil {
					m.nodeDetails = true
					m.nodeDetailsOffset = 0
				}
			case "pgup":
				m.nodeDetailsOffset = maxInt(0, m.nodeDetailsOffset-maxInt(1, m.height-8))
			case "pgdown":
				m.nodeDetailsOffset = minInt(m.nodeDetailMaxOffset(), m.nodeDetailsOffset+maxInt(1, m.height-8))
			case "enter":
				if !m.nodeDetails && !m.nodeLoading && len(m.nodes) > 0 {
					return m, m.connectNode(m.nodes[m.nodeCursor])
				}
			}
			return m, nil
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.choosingDependency {
			return m.updateDependencyPicker(msg)
		}
		if m.observationMode != "" {
			if next, cmd, handled := m.updateObservationKey(msg); handled {
				return next, cmd
			}
		}
		if !m.filtering && !m.choosingDatabase {
			if next, cmd, handled := m.updateDependencyKey(msg); handled {
				return next, cmd
			}
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		if m.choosingDatabase {
			switch msg.String() {
			case "ctrl+c", "q":
				m.cancelColumns()
				return m, tea.Quit
			case "esc", "b":
				m.choosingDatabase = false
			case "up", "k":
				m.databaseCursor = maxInt(0, m.databaseCursor-1)
			case "down", "j":
				m.databaseCursor = minInt(maxInt(0, len(m.databases)-1), m.databaseCursor+1)
			case "enter":
				m.cancelColumns()
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
					return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
				}
				if !m.overview && len(m.views) > 0 && !m.metricsBusy {
					m.metricsBusy = true
					cmd := m.columnCmdIfActive()
					return m, tea.Batch(loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch), cmd)
				}
				if !m.overview {
					cmd := m.columnCmdIfActive()
					return m, cmd
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "x", "a":
			if m.loading || m.err != nil || len(m.views) == 0 || m.dependencies || m.target || m.columns || m.definition || m.failuresMode {
				return m, nil
			}
			m.observationReturnOverview = m.overview
			m.observationWindow = m.statusWindow
			m.overview = false
			mode := "executions"
			if msg.String() == "a" {
				mode = "diagnostics"
			}
			cmd := m.loadObservation(mode)
			return m, cmd
		case "n":
			if m.schemaPath != "" {
				m.choosingNode = true
				m.nodeError = fmt.Errorf("offline: node selection requires a live connection")
				return m, nil
			}
			if !m.loading && m.err == nil {
				m.choosingNode = true
				return m, m.loadNodes()
			}
			return m, nil
		case "w":
			m.statusWindow = (m.statusWindow + 1) % 3
			m.chartEpoch++
			m.metricsReady = false
			m.overviewReady = false
			if len(m.views) > 0 && m.err == nil && !m.loading {
				if m.overview {
					m.overviewBusy = true
					return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
				}
				m.metricsBusy = true
				if m.failuresMode {
					return m, tea.Batch(m.loadFailures(), loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch))
				}
				return m, loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
			}
		case "p":
			m.chartPeriod = (m.chartPeriod + 1) % 3
			m.chartEpoch++
			if len(m.views) > 0 && m.err == nil && !m.loading {
				if m.overview {
					m.overviewReady = false
					m.overviewBusy = true
					return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
				}
				m.metricsReady = false
				m.metricsBusy = true
				return m, loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
			}
		case "s":
			m.chartCommon = !m.chartCommon
		case "g":
			if !m.loading && m.err == nil {
				m.openDependencies()
			}
			return m, nil
		case "i":
			if m.target && !m.targetLoading && m.targetError == nil && len(m.targetData.Columns) > 0 && m.targetColumn >= 0 && m.targetColumn < len(m.targetData.Columns) {
				table := m.targetData.Database + "." + m.targetData.Table
				column := m.targetData.Columns[m.targetColumn].Name
				m.target = false
				m.dependencies = true
				m.dependencyTable = table
				m.impactBackTarget = true
				cmd := m.startImpact(column)
				return m, cmd
			}
			return m, nil
		case "/":
			if !m.loading && m.err == nil {
				m.searching = true
				m.searchQuery = ""
				m.searchCursor = 0
			}
			return m, nil
		case "q", "ctrl+c":
			m.cancelColumns()
			return m, tea.Quit
		case "r":
			m.cancelColumns()
			m.loading, m.err = true, nil
			m.lineOffset, m.columnOffset = 0, 0
			if m.schemaPath != "" {
				return m, loadSchema(m.schemaPath, m.schemaDatabase)
			}
			return m, loadViews(m.dsn)
		case "o":
			m.dependencies = false
			if m.failuresMode {
				m.cancelColumns()
				m.failuresMode = false
			}
			if m.target {
				m.cancelColumns()
				m.target = false
			}
			m.overview = !m.overview
			if m.overview {
				m.cancelColumns()
			}
			m.overviewScroll, m.overviewColumn = 0, 0
			if m.overview && len(m.views) > 0 {
				m.keepOverviewSelectionVisible()
			}
			if m.overview && len(m.views) > 0 && !m.overviewBusy {
				m.overviewBusy = true
				return m, loadOverview(m.dsn, m.database, m.views, m.viewRevision, m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch)
			}
			if !m.overview {
				cmd := m.columnCmdIfActive()
				return m, cmd
			}
		case "enter":
			if m.failuresMode && !m.overview && !m.failuresLoading && len(m.failures) > 0 {
				m.failureDetail = !m.failureDetail
				m.lineOffset = 0
				break
			}
			if m.overview && m.overviewTargetFocus && len(m.views) > 0 {
				return m.Update(mouseKey("t"))
			}
			if m.overview && len(m.views) > 0 {
				m.overview = false
				m.lineOffset, m.columnOffset = 0, 0
				m.metricsReady = false
				if !m.metricsBusy {
					m.metricsBusy = true
					cmd := m.columnCmdIfActive()
					return m, tea.Batch(loadMetrics(m.dsn, m.views[m.selected], m.chartPeriodSeconds(), m.statusWindowHours(), m.chartEpoch), cmd)
				}
				cmd := m.columnCmdIfActive()
				return m, cmd
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
		case "f":
			if !m.loading && m.err == nil {
				m.openFilter()
			}
		case "up", "k":
			if m.failuresMode && !m.overview {
				if !m.failureDetail {
					m.failureSelected = maxInt(0, m.failureSelected-1)
					m.lineOffset = maxInt(0, 4+m.failureSelected-maxInt(1, m.height-12))
				}
				break
			}
			if m.target {
				if m.targetLoading || m.targetError != nil {
					break
				}
				m.targetColumn = maxInt(0, m.targetColumn-1)
				m.lineOffset = 0
				break
			}
			if m.overview {
				if m.overviewTargetFocus {
					m.moveOverviewTarget(-1)
					break
				}
				m.moveOverviewSelection(-1)
				break
			}
			if m.selected > 0 {
				m.selected--
				m.lineOffset = 0
				m.columnOffset = 0
				m.metricsReady = false
				if m.columns || m.target {
					cmd := m.columnCmdIfActive()
					return m, cmd
				}
			}
		case "down", "j":
			if m.failuresMode && !m.overview {
				if !m.failureDetail {
					m.failureSelected = minInt(maxInt(0, len(m.failures)-1), m.failureSelected+1)
					m.lineOffset = maxInt(0, 4+m.failureSelected-maxInt(1, m.height-12))
				}
				break
			}
			if m.target {
				if m.targetLoading || m.targetError != nil {
					break
				}
				m.targetColumn = minInt(maxInt(0, len(m.targetData.Columns)-1), m.targetColumn+1)
				m.lineOffset = 0
				break
			}
			if m.overview {
				if m.overviewTargetFocus {
					m.moveOverviewTarget(1)
					break
				}
				m.moveOverviewSelection(1)
				break
			}
			if m.selected < len(m.views)-1 {
				m.selected++
				m.lineOffset = 0
				m.columnOffset = 0
				m.metricsReady = false
				if m.columns || m.target {
					cmd := m.columnCmdIfActive()
					return m, cmd
				}
			}
		case "esc":
			if m.failuresMode {
				if m.failureDetail {
					m.failureDetail = false
					m.lineOffset = 0
					break
				}
				return m.Update(mouseKey("e"))
			}
			if m.target {
				return m.Update(mouseKey("t"))
			}
			if m.columns {
				return m.Update(mouseKey("c"))
			}
			if m.definition {
				return m.Update(mouseKey("d"))
			}
			if !m.overview && len(m.views) > 0 {
				return m.Update(mouseKey("o"))
			}
		case "e":
			if m.loading || m.err != nil || len(m.views) == 0 {
				break
			}
			if m.failuresMode && !m.overview {
				m.cancelColumns()
				m.failuresMode = false
				previous := m.failuresReturnScreen
				m.failuresReturnScreen = ""
				m.lineOffset = 0
				switch previous {
				case "overview":
					m.overview = true
				case "target":
					m.target = true
					return m, m.loadTarget()
				case "columns":
					m.columns = true
					return m, m.loadColumns()
				case "definition":
					m.definition = true
				case "dependencies":
					m.dependencies = true
				}
				break
			}
			m.failuresReturnScreen = "diagram"
			switch {
			case m.overview:
				m.failuresReturnScreen = "overview"
			case m.target:
				m.failuresReturnScreen = "target"
			case m.columns:
				m.failuresReturnScreen = "columns"
			case m.definition:
				m.failuresReturnScreen = "definition"
			case m.dependencies:
				m.failuresReturnScreen = "dependencies"
			}
			m.failuresMode = true
			m.dependencies = false
			m.target = false
			m.columns = false
			m.definition = false
			m.overview = false
			m.lineOffset = 0
			m.columnOffset = 0
			return m, m.loadFailures()
		case "t":
			if len(m.views) == 0 || m.loading || m.err != nil {
				break
			}
			if m.target {
				m.cancelColumns()
				m.target = false
				if m.targetReturnDependencies {
					m.dependencies = true
					m.targetReturnDependencies = false
					m.targetExplicit = ""
					m.lineOffset = 0
					break
				}
				m.overview = m.targetReturnOverview
				m.targetReturnOverview = false
				m.lineOffset, m.columnOffset = 0, 0
				break
			}
			m.targetReturnOverview = m.overview
			m.targetReturnDependencies = false
			m.targetExplicit = ""
			m.overview = false
			m.target = true
			m.dependencies = false
			m.failuresMode = false
			m.columns = false
			m.definition = false
			m.lineOffset, m.columnOffset = 0, 0
			return m, m.loadTarget()
		case "d":
			if m.overview {
				break
			}
			m.definition = !m.definition
			m.failuresMode = false
			m.target = false
			m.columns = false
			m.cancelColumns()
			m.lineOffset = 0
			m.columnOffset = 0
		case "v":
			if m.definition && !m.overview {
				m.sqlOriginal = !m.sqlOriginal
				m.lineOffset, m.columnOffset = 0, 0
			}
		case "c":
			if m.overview {
				break
			}
			m.columns = !m.columns
			m.failuresMode = false
			m.target = false
			if !m.columns {
				m.cancelColumns()
			}
			m.definition = false
			m.lineOffset, m.columnOffset = 0, 0
			if m.columns {
				cmd := m.loadColumns()
				return m, cmd
			}
		case "pgdown":
			if m.overview {
				lines, _ := m.overviewLines()
				m.overviewScroll = minInt(m.overviewScroll+maxInt(1, m.height-7), maxInt(0, len(lines)-4-maxInt(1, m.height-7)))
				break
			}
			if len(m.views) > 0 {
				visible := m.detailVisibleRows()
				m.lineOffset = minInt(m.lineOffset+visible, maxInt(0, len(m.detailLines())-visible))
			}
		case "pgup":
			if m.overview {
				m.overviewScroll = maxInt(0, m.overviewScroll-maxInt(1, m.height-7))
				break
			}
			m.lineOffset = maxInt(0, m.lineOffset-m.detailVisibleRows())
		case "right", "l", "L", "shift+right":
			if m.overview {
				if (msg.String() == "right" || msg.String() == "l") && !m.overviewTargetFocus {
					if _, ok := m.selectedOverviewTarget(); ok {
						m.overviewTargetFocus = true
						m.keepOverviewTargetVisible()
						break
					}
				}
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
		case "left", "h", "H", "shift+left":
			if m.overview {
				if (msg.String() == "left" || msg.String() == "h") && m.overviewTargetFocus {
					m.overviewTargetFocus = false
					m.overviewColumn = 0
					m.keepOverviewSelectionVisible()
					break
				}
				m.overviewColumn = maxInt(0, m.overviewColumn-8)
				break
			}
			m.columnOffset = maxInt(0, m.columnOffset-8)
		}
	}
	return m, nil
}

func (m *model) columnCmdIfActive() tea.Cmd {
	if m.failuresMode && len(m.views) > 0 {
		return m.loadFailures()
	}
	if m.target && len(m.views) > 0 {
		return m.loadTarget()
	}
	if m.columns && len(m.views) > 0 {
		return m.loadColumns()
	}
	return nil
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
	m.searching = false
	m.overviewTargetFocus = false
	m.viewRevision++
	m.views = nil
	for _, view := range m.allViews {
		if view.Database == m.database && matchesFlow(view, m.filterSource, m.filterTarget) {
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
	return maxInt(28, width-listPanelWidth(width)-5) - 4
}

func listPanelWidth(width int) int { return maxInt(22, width/5) }

func (m model) detailVisibleRows() int {
	height := maxInt(5, m.height-8)
	if m.width < 70 {
		height = maxInt(5, m.height-9)
	}
	return maxInt(1, height-2)
}

func (m model) View() string {
	if !m.modalInner && m.hasModal() {
		return m.renderModal()
	}
	content := m.renderView()
	if m.loading || m.height <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) >= m.height {
		return content
	}
	// All non-loading screens end with their keyboard shortcuts. Keep the
	// content at its original position and fill the space above that footer.
	footer := lines[len(lines)-1]
	lines = lines[:len(lines)-1]
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, footer), "\n")
}

func (m model) renderView() string {
	var b strings.Builder
	width := m.width
	if width <= 0 {
		width = 80
	}

	b.WriteString(accentStyle.Render("ch_insp"))
	if m.schemaPath != "" {
		b.WriteString(mutedStyle.Render("  /  OFFLINE SQL"))
	}
	if m.database != "" {
		b.WriteString(accentStyle.Render("  /  DB: " + m.database))
	}
	if m.nodeName != "" {
		label := "  /  node: " + m.nodeName
		if m.nodeVersion != "" {
			label += " (" + m.nodeVersion + ")"
		}
		b.WriteString(mutedStyle.Render(label))
	}
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(loadingText("connecting and loading materialized views…"))
		return b.String()
	}
	if m.err != nil {
		if m.schemaPath != "" {
			b.WriteString(errorStyle.Render("Could not load SQL dump"))
		} else {
			b.WriteString(errorStyle.Render("Could not load materialized views"))
		}
		b.WriteString("\n")
		b.WriteString(m.err.Error())
		b.WriteString("\n\n")
		if m.schemaPath != "" {
			b.WriteString(mutedStyle.Render("[r] reload SQL dump  [q] quit"))
		} else {
			b.WriteString(mutedStyle.Render("Set CLICKHOUSE_URL.  [r] retry  [q] quit"))
		}
		return b.String()
	}
	if m.choosingNode {
		return b.String() + m.renderNodes(width)
	}
	if m.searching {
		return b.String() + m.renderSearch(width)
	}
	if m.choosingDependency {
		return b.String() + m.renderDependencyPicker(width)
	}
	if m.filtering {
		return b.String() + m.renderFilter(width)
	}
	if m.choosingDatabase {
		b.WriteString(accentStyle.Render("SELECT DATABASE"))
		position := 0
		if len(m.databases) > 0 {
			position = m.databaseCursor + 1
		}
		b.WriteString("\n" + mutedStyle.Render(fmt.Sprintf("%d/%d databases", position, len(m.databases))) + "\n")
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
		b.WriteString("\n" + mutedStyle.Render("[esc] cancel  [enter] switch  [↑]/[↓] select  [q] quit"))
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
		b.WriteString(mutedStyle.Render(ansi.Truncate(m.detailFooter(height), width, "…")))
		return b.String()
	}

	listWidth := listPanelWidth(width)
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
	b.WriteString(mutedStyle.Render(ansi.Truncate(m.detailFooter(height), width, "…")))
	return b.String()
}

func (m model) detailFooter(height int) string {
	footer := "[esc] back  [o] overview  [x] runs  [a] diagnose  [g] graph  [e] errors  [t] target  [c] columns  [d] SQL"
	switch {
	case m.observationMode == "executions":
		footer = "[esc] back  [enter] details  [w] window  [r] refresh  [a] diagnostics"
	case m.observationMode == "diagnostics":
		footer = "[esc] back  [r] refresh  [x] executions  [PgUp]/[PgDn] scroll"
	case m.definition:
		footer = "[esc] back  [v] original/formatted  [PgUp]/[PgDn] scroll  [←]/[→] horizontal"
	case m.columns:
		footer = "[esc] back  [d] SQL  [PgUp]/[PgDn] scroll  [←]/[→] horizontal"
	case m.target:
		footer = "[esc] back  [↑]/[↓] column  [i] impact  [g] dependencies  [PgUp]/[PgDn] scroll"
	case m.dependencies:
		if m.impact {
			footer = "[esc] back  [enter] open MV  [/] filter  [←]/[→] scroll"
		} else {
			footer = "[esc] back  [enter] open  [i] impact  [t] columns  [backspace] previous"
		}
	case m.failuresMode:
		footer = "[esc] back  [enter] message  [w] window  [PgUp]/[PgDn] scroll"
	}
	if len(m.views) > 0 {
		lines := m.detailLines()
		visible := maxInt(1, height-2)
		maxOffset := maxInt(0, len(lines)-visible)
		longest := 0
		for _, line := range lines {
			longest = maxInt(longest, ansi.StringWidth(line))
		}
		if maxOffset > 0 || longest > m.detailWidth() {
			position := fmt.Sprintf("%d/%d ↓  ↔%d/%d", minInt(m.lineOffset, maxOffset)+1, maxOffset+1, m.columnOffset+1, maxInt(1, longest-m.detailWidth()+1))
			footer = "[esc] back  " + position + "  " + strings.TrimPrefix(footer, "[esc] back  ")
		}
	}
	return footer
}

func (m model) renderList(width, height int) string {
	var b strings.Builder
	title := "MATERIALIZED VIEWS"
	if len(m.filterSource) > 0 || len(m.filterTarget) > 0 {
		title += " · FILTERED"
	}
	b.WriteString(accentStyle.Render(truncate(title, width)))
	b.WriteString("\n")
	if len(m.views) == 0 {
		b.WriteString(mutedStyle.Render("No matching materialized views."))
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
			b.WriteString(activeStyle.Width(width).Render("› " + label))
		} else {
			b.WriteString("  " + label)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// overviewLines renders a single graph for the selected database.
func (m model) overviewLines() ([]string, []int) {
	width := m.width
	if width <= 0 {
		width = 80
	}
	title := m.overviewTitle(width)
	header := []string{accentStyle.Render(title), mutedStyle.Render("Rates: rows/s, 60s avg · charts: " + m.chartPeriodLabel() + " · " + m.chartScaleLabel() + " · local node"), mutedStyle.Render("[T] inferred trigger · [I] input · R/W read/write · err " + m.statusWindowLabel() + " observed · 0 zero · · no event"), ""}
	if m.schemaPath != "" {
		warnings := 0
		if m.dump != nil {
			warnings = len(m.dump.Warnings)
		}
		header[1] = mutedStyle.Render(fmt.Sprintf("OFFLINE SQL · rates and execution status unavailable · %d import warnings", warnings))
		header[2] = mutedStyle.Render("[T] inferred trigger · [I] other input · [r] reload dump")
	}
	if len(m.views) == 0 {
		header = append(header, emptyText("No matching materialized views."))
		if m.dump != nil {
			for _, warning := range m.dump.Warnings {
				header = append(header, errorStyle.Render("SQL dump: "+warning))
			}
		}
		return header, nil
	}
	allViews := m.allViews
	if allViews == nil {
		allViews = m.views
	}
	rates := m.overviewRates
	rates.SharedScale = m.chartCommon
	graph, positions := overviewGraphFocused(m.views, rates, m.overviewReady, m.selected, overviewColors(allViews, m.database), m.overviewTargetFocus)
	for i := range positions {
		positions[i] += len(header)
	}
	lines := append(header, graph...)
	if m.dump != nil {
		for _, warning := range m.dump.Warnings {
			lines = append(lines, errorStyle.Render("SQL dump: "+warning))
		}
	}
	if m.overviewReady {
		if m.overviewRates.StatusError != "" {
			lines = append(lines, errorStyle.Render("MV status: "+m.overviewRates.StatusError))
		}
		for _, err := range m.overviewRates.Errors {
			lines = append(lines, errorStyle.Render("Metrics: "+err))
		}
	}
	return lines, positions
}

// Show both sides of the active filter even when the terminal is narrow.
// Full database-qualified names avoid ambiguity across schemas.
func (m model) overviewTitle(width int) string {
	const title = "FLOW OVERVIEW"
	if len(m.filterSource) == 0 && len(m.filterTarget) == 0 {
		return title
	}
	sourceLabel, targetLabel := "Source: ", "Target: "
	if width < 48 {
		sourceLabel, targetLabel = "S: ", "T: "
	}
	prefix, separator := title+" · ", " · "
	available := maxInt(0, width-ansi.StringWidth(prefix+sourceLabel+separator+targetLabel))
	sourceNeed := ansi.StringWidth(filterSummary(m.filterSource, width))
	targetNeed := ansi.StringWidth(filterSummary(m.filterTarget, width))
	sourceWidth := minInt(sourceNeed, available/2)
	targetWidth := minInt(targetNeed, available-sourceWidth)
	if spare := available - sourceWidth - targetWidth; spare > 0 {
		sourceWidth += minInt(spare, sourceNeed-sourceWidth)
		targetWidth += minInt(available-sourceWidth-targetWidth, targetNeed-targetWidth)
	}
	return prefix + sourceLabel + filterSummary(m.filterSource, sourceWidth) + separator + targetLabel + filterSummary(m.filterTarget, targetWidth)
}

func filterSummary(names []string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(names) == 0 {
		return ansi.Truncate("any", width, "…")
	}
	best, joined := "", ""
	for i, name := range names {
		if i > 0 {
			joined += ", "
		}
		joined += name
		value := joined
		if remaining := len(names) - i - 1; remaining > 0 {
			value += fmt.Sprintf(" +%d", remaining)
		}
		if ansi.StringWidth(value) > width {
			break
		}
		best = value
	}
	if best != "" {
		return best
	}
	suffix := ""
	if len(names) > 1 {
		suffix = fmt.Sprintf(" +%d", len(names)-1)
	}
	if ansi.StringWidth(suffix) >= width {
		return ansi.Truncate(fmt.Sprintf("%d selected", len(names)), width, "…")
	}
	return ansi.Truncate(names[0], width-ansi.StringWidth(suffix), "…") + suffix
}

func (m *model) keepOverviewSelectionVisible() {
	_, starts := m.overviewLines()
	if m.selected >= len(starts) {
		return
	}
	visible := maxInt(1, m.height-7)
	line := starts[m.selected] - 4
	if line < m.overviewScroll {
		m.overviewScroll = line
	} else if line+1 >= m.overviewScroll+visible {
		m.overviewScroll = line
	}
}

func (m model) renderOverview(width int) string {
	lines, _ := m.overviewLines()
	visible := maxInt(1, m.height-7)
	start := minInt(m.overviewScroll, maxInt(0, len(lines)-4-visible))
	end := minInt(len(lines), 4+start+visible)
	header := append([]string(nil), lines[:4]...)
	for i := range header {
		header[i] = ansi.Truncate(header[i], maxInt(1, width), "…")
	}
	longest := 0
	for _, line := range lines[4:] {
		longest = maxInt(longest, ansi.StringWidth(line))
	}
	if len(lines)-4 > visible || longest > width {
		header[3] = mutedStyle.Render(ansi.Truncate(fmt.Sprintf("Scroll: row %d/%d · column %d/%d", start+1, maxInt(1, len(lines)-4-visible+1), m.overviewColumn+1, maxInt(1, longest-width+1)), width, "…"))
	}
	content := append([]string(nil), lines[4+start:end]...)
	for i := range content {
		content[i] = ansi.Cut(content[i], m.overviewColumn, m.overviewColumn+maxInt(1, width))
	}
	padding := 0
	if m.height > 0 {
		padding = maxInt(0, visible-len(content))
	}
	footer := "[enter] open  [x] runs  [a] diagnose  [f] filter  [/] search  [w] error window  [n] node  [p] period  [s] scale  [g] graph  [e] errors  [o] back  [t] target  [q] quit"
	result := append(header, content...)
	for i := 0; i < padding; i++ {
		result = append(result, "")
	}
	result = append(result, mutedStyle.Render(ansi.Truncate(footer, width, "…")))
	return strings.Join(result, "\n")
}

func (m model) renderDetail(width, height int) string {
	if len(m.views) == 0 {
		lines := []string{mutedStyle.Render("No materialized views in the selected database.")}
		if m.dump != nil {
			for _, warning := range m.dump.Warnings {
				lines = append(lines, errorStyle.Render("SQL dump: "+warning))
			}
		}
		return strings.Join(lines, "\n")
	}
	lines := m.detailLines()
	visible := maxInt(1, height-2)
	start := minInt(m.lineOffset, maxInt(0, len(lines)-visible))
	end := minInt(1+start+maxInt(0, visible-1), len(lines))
	header := ansi.Truncate(lines[0], width, "…")
	for i := 1 + start; i < end; i++ {
		lines[i] = ansi.Cut(strings.TrimRight(lines[i], "\r"), m.columnOffset, m.columnOffset+width)
	}
	return strings.Join(append([]string{header}, lines[1+start:end]...), "\n")
}

func (m model) detailLines() []string {
	view := m.views[m.selected]
	var lines []string
	if m.observationMode != "" {
		return splitColumnLines(m.observationLines())
	} else if m.dependencies {
		lines := m.dependencyLines()
		if m.dependencyMessage != "" {
			lines = append(lines, errorStyle.Render(m.dependencyMessage))
		}
		return splitColumnLines(lines)
	} else if m.failuresMode {
		return splitColumnLines(m.failureLines())
	} else if m.target {
		return splitColumnLines(m.targetLines())
	} else if m.columns {
		return splitColumnLines(m.columnLines())
	} else if m.definition {
		lines = append(lines, accentStyle.Render("CREATE DEFINITION · "+view.Name))
		definition := view.Definition
		label := "Original SQL · [v] formatted"
		if !m.sqlOriginal {
			var ok bool
			definition, ok = formatSQL(definition)
			label = "Formatted SQL · [v] original"
			if !ok {
				label = "Formatting unavailable · original SQL"
			}
		}
		lines = append(lines, mutedStyle.Render(label))
		lines = append(lines, strings.Split(definition, "\n")...)
	} else {
		lines = append(lines, accentStyle.Render("DATA FLOW · "+view.Name), mutedStyle.Render("Inferred from SQL · arrows show query inputs"), "")
		if m.dump != nil {
			for _, warning := range m.dump.Warnings {
				lines = append(lines, errorStyle.Render("SQL dump: "+warning))
			}
		}
		lines = append(lines, diagramLines(view, m.rates, m.metricsReady)...)
		if clickhouse.InferFlow(view).Trigger == "" {
			lines = append(lines, mutedStyle.Render("Trigger unknown; inspect the MV definition."))
		}
		if m.metricsReady {
			status := statusText(m.rates.Status, m.rates.StatusError, m.statusWindowHours())
			if m.schemaPath != "" {
				status = "execution status: offline"
			}
			lines = append(lines, "", mutedStyle.Render(status), mutedStyle.Render("[e] MV errors"))
			if m.rates.StatusError != "" {
				lines = append(lines, errorStyle.Render("Status: "+m.rates.StatusError))
			}
		}
		if m.schemaPath != "" {
			lines = append(lines, "", mutedStyle.Render("OFFLINE: metrics and execution logs are unavailable."))
		} else {
			lines = append(lines, "", mutedStyle.Render("Local logs: rates 60s average; charts: "+m.chartPeriodLabel()+"."), mutedStyle.Render("Bars: oldest → newest · 0: observed zero · ·: no event; logs may be delayed."), mutedStyle.Render("inserted/total: system.part_log · MV: system.query_views_log"), mutedStyle.Render("JOIN inputs may not trigger incremental MVs."))
		}
		if m.metricsReady {
			for _, err := range m.rates.Errors {
				lines = append(lines, errorStyle.Render("Metrics: "+err))
			}
		}
		lines = append(lines, mutedStyle.Render("[d] SQL definition · [←]/[→] scroll diagram"))
	}
	return lines
}

func diagramLines(view clickhouse.View, rates clickhouse.Rates, ready bool) []string {
	lines, _ := diagramFlowLines(view, rates, ready, true)
	return lines
}

func diagramFlowLines(view clickhouse.View, rates clickhouse.Rates, ready, includeTarget bool) ([]string, int) {
	return diagramFlowLinesStyled(view, rates, ready, includeTarget, sourceStyle, viewStyle, targetStyle, mutedStyle)
}

func diagramFlowLinesStyled(view clickhouse.View, rates clickhouse.Rates, ready, includeTarget bool, sourceColor, mvColor, targetColor, lineColor lipgloss.Style) ([]string, int) {
	flow := clickhouse.InferFlow(view)
	sources := flow.Sources
	if len(sources) == 0 {
		sources = []string{"? unknown source"}
	}
	rate := func(name string) string {
		if !ready {
			return readMetricStyle.Render("inserted: loading")
		}
		v, ok := rates.Tables[name]
		if !ok {
			if label := metricStateLabel(rates.TableDiagnostics[name]); label != "" {
				return readMetricStyle.Render("inserted: " + label)
			}
			return readMetricStyle.Render("inserted: no data")
		}
		return readMetricStyle.Render(fmt.Sprintf("inserted: %.1f rows/s", v))
	}
	sourceWidth := len("QUERY INPUT")
	sourceColumnWidth := 0
	sourceLabel := func(source string) string {
		label := shortName(source)
		if flow.Trigger != "" {
			if source == flow.Trigger {
				label += " [trigger]"
			} else {
				label += " [input]"
			}
		}
		return label
	}
	for _, source := range sources {
		sourceWidth = maxInt(sourceWidth, lipgloss.Width(sourceLabel(source)))
		sourceColumnWidth = maxInt(sourceColumnWidth, lipgloss.Width(rate(source)))
	}
	sourceColumnWidth = maxInt(sourceColumnWidth, sourceWidth+4)
	mvMetric := mvRateText(ready, rates.MVAvailable, rates.MVRead, rates.MVWritten, rates.MVHistory)
	if ready && rates.MVDiagnostic.State == clickhouse.MetricOffline {
		mvMetric = mutedStyle.Render("read/write: offline")
	} else if ready && !rates.MVAvailable {
		if label := metricStateLabel(rates.MVDiagnostic); label != "" {
			mvMetric += " · " + mutedStyle.Render(label)
		}
	}
	mvMetric = strings.Replace(mvMetric, " / ", "\n", 1)
	mv := diagramBox("MATERIALIZED VIEW", view.Name, mvMetric, 0, 0, true, mvColor)
	targetMV := "MV: no data"
	targetValue, targetAvailable := rates.Tables[flow.Target]
	targetHistory, targetHistoryAvailable := rates.TableHistory[flow.Target]
	targetTotal := totalRateText(ready, targetValue, targetAvailable, targetHistory, targetHistoryAvailable)
	if ready && rates.TableDiagnostics[flow.Target].State == clickhouse.MetricOffline {
		targetTotal = writeMetricStyle.Render("total: offline")
	} else if ready && !targetAvailable {
		if label := metricStateLabel(rates.TableDiagnostics[flow.Target]); label != "" {
			targetTotal += " · " + mutedStyle.Render(label)
		}
	}
	if !ready {
		targetMV = "MV: loading"
	} else if rates.MVDiagnostic.State == clickhouse.MetricOffline {
		targetMV = "MV: offline"
	} else if rates.MVAvailable {
		targetMV = fmt.Sprintf("MV: %.1f rows/s", rates.MVWritten)
	}
	target := diagramBox("TARGET", shortName(flow.Target), writeMetricStyle.Render(targetMV)+"\n"+targetTotal, 0, 0, false, targetColor)
	middle := (len(sources)/2)*5 + 2
	top := middle - 2
	total := maxInt(len(sources)*5, top+maxInt(len(mv), len(target)))
	lines := make([]string, total)
	for row := 0; row < total; row++ {
		left := strings.Repeat(" ", sourceColumnWidth)
		if row < len(sources)*5 {
			box := diagramBox("QUERY INPUT", sourceLabel(sources[row/5]), rate(sources[row/5]), sourceWidth, sourceColumnWidth, true, sourceColor)
			left = box[row%5]
		}
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
		line := left + lineColor.Render(branch)
		if row >= top && row < top+maxInt(len(mv), len(target)) {
			connector := "     "
			if row == middle {
				connector = "────▶"
			}
			if row-top < len(mv) {
				line += mv[row-top]
			} else {
				line += strings.Repeat(" ", lipgloss.Width(mv[0]))
			}
			if includeTarget {
				line += lineColor.Render(connector)
				if row-top < len(target) {
					line += target[row-top]
				}
			}
		}
		lines[row] = line
	}
	return lines, middle
}

func shortName(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func diagramBox(label, name, metric string, width, columnWidth int, outgoing bool, style lipgloss.Style) []string {
	width = maxInt(width, maxInt(lipgloss.Width(label), lipgloss.Width(name)))
	metrics := strings.Split(metric, "\n")
	columnWidth = maxInt(columnWidth, width+4)
	for _, line := range metrics {
		columnWidth = maxInt(columnWidth, lipgloss.Width(line))
	}
	pad := func(s string) string { return s + strings.Repeat(" ", maxInt(0, width-lipgloss.Width(s))) }
	gap := columnWidth - width - 4
	lineGap := strings.Repeat(" ", gap)
	if outgoing {
		lineGap = strings.Repeat("─", gap)
	}
	lines := []string{
		style.Render("╭─"+strings.Repeat("─", width)+"─╮") + strings.Repeat(" ", gap),
		style.Render("│ "+pad(label)+" │") + strings.Repeat(" ", gap),
		style.Bold(true).Render("│ "+pad(name)+" │") + mutedStyle.Render(lineGap),
		style.Render("╰─"+strings.Repeat("─", width)+"─╯") + strings.Repeat(" ", gap),
	}
	for _, line := range metrics {
		lines = append(lines, mutedStyle.Render(line)+strings.Repeat(" ", maxInt(0, columnWidth-lipgloss.Width(line))))
	}
	return lines
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

func (m model) chartPeriodSeconds() int {
	switch m.chartPeriod {
	case 1:
		return 300
	case 2:
		return 3600
	default:
		return 60
	}
}
func (m model) chartPeriodLabel() string {
	label := "5 × 1m completed"
	switch m.chartPeriod {
	case 1:
		label = "5 × 5m completed"
	case 2:
		label = "5 × 1h completed"
	}
	var end time.Time
	if m.overview && m.overviewReady {
		end = m.overviewRates.ChartWindowEnd
	} else if !m.overview && m.metricsReady {
		end = m.rates.ChartWindowEnd
	}
	if end.IsZero() {
		return label
	}
	start := end.Add(-time.Duration(5*m.chartPeriodSeconds()) * time.Second)
	format := "15:04"
	if start.Year() != end.Year() || start.YearDay() != end.YearDay() {
		format = "01-02 15:04"
	}
	_, startOffset := start.Zone()
	_, endOffset := end.Zone()
	if startOffset != endOffset {
		return label + " " + start.Format(format+" -07:00") + "–" + end.Format(format+" -07:00")
	}
	return label + " " + start.Format(format) + "–" + end.Format(format) + " " + end.Format("MST")
}

func (m model) chartScaleLabel() string {
	if m.chartCommon {
		if m.overview {
			r, w, t := chartMaxima(m.views, m.overviewRates)
			return fmt.Sprintf("shared max r:%d w:%d total:%d", r, w, t)
		}
		return "individual scale (detail)"
	}
	return "individual scale"
}

func (m model) statusWindowHours() int {
	switch m.statusWindow {
	case 1:
		return 1
	case 2:
		return 168
	default:
		return 24
	}
}
func (m model) statusWindowLabel() string { return statusHoursLabel(m.statusWindowHours()) }
