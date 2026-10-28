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
 INSERT INTO view_events VALUES ('db.mv',today(),now(),'Materialized','QueryFinish'),('db.mv',today(),now(),'Materialized','ExceptionWhileProcessing'),('db.mv',today(),now(),'Materialized','ExceptionBeforeStart'),('db.mv',today(),now(),'Default','ExceptionWhileProcessing'); `
	query := strings.ReplaceAll(viewStatusQuery("'db.mv'"), "system.query_views_log", "view_events")
	out, err := exec.Command(binary, "local", "--multiquery", "--query", setup+query).CombinedOutput()
	if err != nil {
		t.Fatalf("clickhouse local: %v: %s", err, out)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) != 4 || parts[0] != "db.mv" || parts[2] != "1" || parts[3] != "2" {
		t.Fatalf("wrong status counts: %q", out)
	}
}
