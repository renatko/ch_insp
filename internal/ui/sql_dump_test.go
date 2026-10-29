package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ch_insp/internal/clickhouse"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestOfflineSQLNavigation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")
	sql := `CREATE TABLE db.src (id UInt64) ENGINE=Null; CREATE TABLE db.dst (id UInt64) ENGINE=MergeTree ORDER BY id; CREATE MATERIALIZED VIEW db.mv TO db.dst AS SELECT id FROM db.src;`
	if err := os.WriteFile(path, []byte(sql), 0600); err != nil {
		t.Fatal(err)
	}
	m := model{dsn: "offline:", schemaPath: path, loading: true, width: 120, height: 25}
	msg := m.Init()()
	next, cmd := m.Update(msg)
	m = next.(model)
	if m.err != nil || m.dump == nil || len(m.views) != 1 {
		t.Fatalf("offline catalog: %#v", m)
	}
	if cmd == nil {
		t.Fatal("offline metrics command missing")
	}
	loaded := loadMetrics(m.dsn, m.views[0], 60, 24, m.chartEpoch)().(metricsLoadedMsg)
	if loaded.rates.MVDiagnostic.State != clickhouse.MetricOffline {
		t.Fatal("offline metrics not marked")
	}
	m.rates, m.metricsReady = loaded.rates, true
	if !strings.Contains(ansi.Strip(m.View()), "OFFLINE SQL") || !strings.Contains(ansi.Strip(strings.Join(m.detailLines(), "\n")), "offline") {
		t.Fatal("offline mode not visible")
	}
	next, cmd = m.Update(mouseKey("c"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("offline lineage command missing")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.columnsError != nil || len(m.columnsData.Columns) != 1 || m.columnsData.Columns[0].TargetType != "UInt64" {
		t.Fatalf("offline lineage: %#v %v", m.columnsData, m.columnsError)
	}
	m.columns = false
	m.target = true
	cmd = m.loadTarget()
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.targetError != nil || len(m.targetData.Columns) != 1 {
		t.Fatalf("offline target: %#v %v", m.targetData, m.targetError)
	}
	m.target = false
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(model)
	if !m.choosingNode || m.nodeError == nil || !strings.Contains(m.nodeError.Error(), "offline") {
		t.Fatal("node selection not marked offline")
	}
}
