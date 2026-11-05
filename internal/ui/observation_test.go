package ui

import (
	"strings"
	"testing"
	"time"

	"ch_insp/internal/clickhouse"
)

func TestObservationHistoryNavigationAndWindow(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT id FROM src"}
	m := model{width: 120, height: 30, overview: true, views: []clickhouse.View{view}, allViews: []clickhouse.View{view}, viewRevision: 7}
	next, cmd := m.Update(mouseKey("x"))
	m = next.(model)
	if m.observationMode != "executions" || !m.observationLoading || cmd == nil {
		t.Fatal("history did not open")
	}
	stale := observationLoadedMsg{mode: "executions", key: "db\x00mv", request: m.columnsRequest - 1, revision: m.viewRevision}
	next, _ = m.Update(stale)
	m = next.(model)
	if !m.observationLoading {
		t.Fatal("stale result applied")
	}
	data := observationLoadedMsg{mode: "executions", key: "db\x00mv", request: m.columnsRequest, revision: m.viewRevision, executions: []clickhouse.ViewExecution{{Time: time.Unix(100, 0), Status: "QueryFinish", DurationMS: 42, ReadRows: 12, WrittenRows: 2, QueryID: "id1"}, {Time: time.Unix(99, 0), Status: "ExceptionWhileProcessing", Code: 123, Message: "original error\nsecond line"}}}
	next, _ = m.Update(data)
	m = next.(model)
	if m.observationLoading || !strings.Contains(strings.Join(m.observationLines(), "\n"), "42 ms") {
		t.Fatal("history result missing")
	}
	next, _ = m.Update(mouseKey("down"))
	m = next.(model)
	next, _ = m.Update(mouseKey("enter"))
	m = next.(model)
	if !m.observationDetail || !strings.Contains(strings.Join(m.observationLines(), "\n"), "original error\nsecond line") {
		t.Fatal("failure detail missing")
	}
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if m.observationDetail {
		t.Fatal("did not return to list")
	}
	next, cmd = m.Update(mouseKey("w"))
	m = next.(model)
	if cmd == nil || m.observationWindowHours() != 1 || m.statusWindowHours() != 24 {
		t.Fatal("history window did not change independently")
	}
	next, _ = m.Update(mouseKey("esc"))
	m = next.(model)
	if m.observationMode != "" || !m.overview {
		t.Fatal("did not return to overview")
	}
}

func TestDiagnosticsOfflineAndPartialData(t *testing.T) {
	view := clickhouse.View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM src"}
	m := model{width: 120, height: 30, overview: true, schemaPath: "fixture.sql", views: []clickhouse.View{view}, allViews: []clickhouse.View{view}}
	next, cmd := m.Update(mouseKey("a"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("diagnostics did not load")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	text := strings.Join(m.observationLines(), "\n")
	for _, want := range []string{"db.src", "inferred trigger", "db.dst", "unavailable offline", "not MV execution counts"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	m.observationDiagnostics.Offline = false
	m.observationDiagnostics.ViewError = "view log: access denied"
	text = strings.Join(m.observationLines(), "\n")
	if !strings.Contains(text, "access denied") || !strings.Contains(text, "db.src") {
		t.Fatal("partial result lost when view log fails")
	}
	next, cmd = m.Update(mouseKey("x"))
	m = next.(model)
	if m.observationMode != "executions" || cmd == nil {
		t.Fatal("history transition failed")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.observationError == nil || !strings.Contains(m.observationError.Error(), "offline") {
		t.Fatal("offline history not explained")
	}
}
