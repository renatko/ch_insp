package clickhouse

import (
	"os/exec"
	"strings"
	"testing"
)

// Exercise the real ClickHouse expressions at every bucket boundary when the
// standalone binary is available. No server or existing database is modified.
func TestHistoryQueriesWithClickHouseLocal(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse local unavailable")
	}
	viewQuery := strings.ReplaceAll(viewHistoryQuery("'db.mv'"), "system.query_views_log", "view_events")
	partQuery := strings.ReplaceAll(tableHistoryQuery("'db.dst'"), "system.part_log", "part_events")
	setup := `CREATE TABLE view_events (view_name String, event_date Date, event_time DateTime, view_type String, status String, read_rows UInt64, written_rows UInt64) ENGINE=Memory;
 CREATE TABLE part_events (database String, table String, event_date Date, event_time DateTime, event_type String, error UInt16, rows UInt64) ENGINE=Memory;
 INSERT INTO view_events SELECT 'db.mv', today(), now()-toIntervalSecond(n), 'Materialized', 'QueryFinish', 1, 2 FROM (SELECT arrayJoin([300,299,240,180,120,60,0]) AS n);
 INSERT INTO part_events SELECT 'db', 'dst', today(), now()-toIntervalSecond(n), 'NewPart', 0, 3 FROM (SELECT arrayJoin([300,299,240,180,120,60,0]) AS n); `
	out, err := exec.Command(binary, "local", "--multiquery", "--query", setup+viewQuery+"; "+partQuery).CombinedOutput()
	if err != nil {
		t.Fatalf("clickhouse local: %v: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	want := "db.mv\t2\t4\t1\t2\t1\t2\t1\t2\t1\t2\ndb.dst\t6\t3\t3\t3\t3"
	if got != want {
		t.Fatalf("unexpected buckets: %s", got)
	}
}
