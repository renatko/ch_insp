package clickhouse

import (
	"os/exec"
	"strings"
	"testing"
)

func TestViewStatusQueryWithClickHouseLocal(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse local unavailable")
	}
	setup := `CREATE TABLE view_events (view_name String, event_date Date, event_time DateTime, view_type String, status String) ENGINE=Memory;
 INSERT INTO view_events VALUES ('db.mv',today(),now(),'Materialized','QueryFinish'),('db.mv',today(),now(),'Materialized','ExceptionWhileProcessing'),('db.mv',today(),now(),'Materialized','ExceptionBeforeStart'),('db.mv',today(),now(),'Default','ExceptionWhileProcessing');
 INSERT INTO view_events SELECT 'db.mv', toDate(now()-INTERVAL 2 HOUR),now()-INTERVAL 2 HOUR,'Materialized',arrayJoin(['QueryFinish','ExceptionWhileProcessing']);
 INSERT INTO view_events SELECT 'db.mv', toDate(now()-INTERVAL 25 HOUR),now()-INTERVAL 25 HOUR,'Materialized','ExceptionBeforeStart'; `
	for _, tc := range []struct {
		hours         int
		success, fail string
	}{{1, "1", "2"}, {24, "2", "3"}, {168, "2", "4"}} {
		query := strings.ReplaceAll(viewStatusQuery("'db.mv'", tc.hours), "system.query_views_log", "view_events")
		out, err := exec.Command(binary, "local", "--multiquery", "--query", setup+query).CombinedOutput()
		if err != nil {
			t.Fatalf("clickhouse local: %v: %s", err, out)
		}
		parts := strings.Split(strings.TrimSpace(string(out)), "\t")
		if len(parts) != 4 || parts[0] != "db.mv" || parts[2] != tc.success || parts[3] != tc.fail {
			t.Fatalf("wrong status counts for %dh: %q", tc.hours, out)
		}
	}
}
