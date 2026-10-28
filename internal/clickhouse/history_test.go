package clickhouse

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise the real ClickHouse expressions at every bucket boundary when the
// standalone binary is available. No server or existing database is modified.
func TestHistoryQueriesWithClickHouseLocal(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse local unavailable")
	}
	for _, period := range []int{60, 300, 3600} {
		period := period
		t.Run(fmt.Sprintf("%ds", period), func(t *testing.T) {
			end := time.Now().UTC().Truncate(time.Hour)
			anchor := fmt.Sprintf("toDateTime(%d)", end.Unix())
			viewQuery := strings.ReplaceAll(viewHistoryQueryAt("'db.mv'", period, end), "system.query_views_log", "view_events")
			partQuery := strings.ReplaceAll(tableHistoryQueryAt("'db.dst'", period, end), "system.part_log", "part_events")
			setup := `CREATE TABLE view_events (view_name String, event_date Date, event_time DateTime, view_type String, status String, read_rows UInt64, written_rows UInt64) ENGINE=Memory;
   CREATE TABLE part_events (database String, table String, event_date Date, event_time DateTime, event_type String, error UInt16, rows UInt64) ENGINE=Memory; `
			timestamp := anchor + "-toIntervalSecond(n)"
			values := fmt.Sprintf("%d,%d,%d,%d,%d,%d,0", 5*period, 5*period-1, 4*period, 3*period, 2*period, period)
			setup += fmt.Sprintf("INSERT INTO view_events SELECT 'db.mv', toDate(%s), %s, 'Materialized', 'QueryFinish', 1, 2 FROM (SELECT arrayJoin([%s]) AS n);", timestamp, timestamp, values)
			setup += fmt.Sprintf("INSERT INTO part_events SELECT 'db', 'dst', toDate(%s), %s, 'NewPart', 0, 3 FROM (SELECT arrayJoin([%s]) AS n);", timestamp, timestamp, values)
			out, err := exec.Command(binary, "local", "--multiquery", "--query", setup+viewQuery+"; "+partQuery).CombinedOutput()
			if err != nil {
				t.Fatalf("clickhouse local: %v: %s", err, out)
			}
			got := strings.TrimSpace(string(out))
			want := "db.mv\t2\t4\t1\t2\t1\t2\t1\t2\t1\t2\ndb.dst\t6\t3\t3\t3\t3"
			if got != want {
				t.Fatalf("unexpected buckets: %s", got)
			}
		})
	}

}
