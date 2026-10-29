package clickhouse

import (
	"os/exec"
	"strings"
	"testing"
)

// Check both rate queries with actual ClickHouse expressions, including clock skew.
func TestRateQueriesExcludeFutureAndOldEvents(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse local unavailable")
	}
	setup := `CREATE TABLE part_events (database String, table String, event_date Date, event_time DateTime, event_type String, error UInt16, rows UInt64) ENGINE=Memory;
CREATE TABLE view_events (view_name String, event_date Date, event_time DateTime, view_type String, status String, read_rows UInt64, written_rows UInt64) ENGINE=Memory;
INSERT INTO part_events SELECT 'db','src',toDate(now()+toIntervalSecond(n)),now()+toIntervalSecond(n),'NewPart',0,1 FROM (SELECT arrayJoin([-90,-30,30]) AS n);
INSERT INTO view_events SELECT 'db.mv',toDate(now()+toIntervalSecond(n)),now()+toIntervalSecond(n),'Materialized','QueryFinish',2,3 FROM (SELECT arrayJoin([-90,-30,30]) AS n);`
	parts := strings.ReplaceAll(tableRateQuery("'db.src'"), "system.part_log", "part_events")
	views := strings.ReplaceAll(viewRateQuery("'db.mv'"), "system.query_views_log", "view_events")
	out, err := exec.Command(binary, "local", "--multiquery", "--query", setup+parts+";"+views).CombinedOutput()
	if err != nil {
		t.Fatalf("clickhouse local: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "db\tsrc\t1\ndb.mv\t2\t3" {
		t.Fatalf("incorrect 60-second window: %q", got)
	}
}
