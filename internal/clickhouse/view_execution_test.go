package clickhouse

import (
	"context"
	"database/sql"
	"os/exec"
	"strings"
	"testing"
)

func TestViewExecutionsQueryWithClickHouseLocal(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse local unavailable")
	}
	setup := `CREATE TABLE events (event_date Date,event_time DateTime,event_time_microseconds DateTime64(6),status String,view_duration_ms UInt64,read_rows UInt64,written_rows UInt64,exception_code Int32,exception String,initial_query_id String,view_type String,view_name String) ENGINE=Memory;
 INSERT INTO events SELECT today(),now(),now64(6),arrayJoin(['QueryStart','QueryFinish','ExceptionWhileProcessing']),42,100,7,3,'failure','query-id','Materialized','db.mv';
 INSERT INTO events SELECT toDate(now()-INTERVAL 2 HOUR),now()-INTERVAL 2 HOUR,now64(6)-INTERVAL 2 HOUR,'QueryFinish',1,1,1,0,'','old','Materialized','db.mv';
 INSERT INTO events SELECT today(),now()+INTERVAL 1 HOUR,now64(6)+INTERVAL 1 HOUR,'QueryFinish',1,1,1,0,'','future','Materialized','db.mv';`
	query := strings.ReplaceAll(viewExecutionsQuery(1), "system.query_views_log", "events")
	query = strings.Replace(query, "view_name = ?", "view_name = 'db.mv'", 1)
	out, err := exec.Command(binary, "local", "--multiquery", "--query", setup+query).CombinedOutput()
	if err != nil {
		t.Fatalf("query failed: %v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || !strings.Contains(string(out), "QueryFinish\t42\t100\t7") || !strings.Contains(string(out), "ExceptionWhileProcessing\t42\t100\t7") {
		t.Fatalf("unexpected events: %s", out)
	}
}

func TestFlowDiagnosticsKeepsIndependentErrors(t *testing.T) {
	db, err := sql.Open("chinspector_failing_batch", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	view := View{Database: "db", Name: "mv", Definition: "CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM src"}
	data := collectFlowDiagnostics(context.Background(), db, StaticFlowDiagnostics(view, []View{view}), view)
	if !strings.Contains(data.PartError, "test log unavailable") || !strings.Contains(data.ViewError, "test log unavailable") {
		t.Fatalf("missing underlying failures: %+v", data)
	}
	if len(data.Sources) != 1 || data.Sources[0].State != MetricLogError || data.Target == nil || data.Target.State != MetricLogError {
		t.Fatalf("incorrect availability: %+v", data)
	}
}

func TestStaticFlowDiagnosticsCountsOnlyKnownWriters(t *testing.T) {
	view := View{Database: "db", Name: "one", Definition: "CREATE MATERIALIZED VIEW one TO dst AS SELECT * FROM src"}
	other := View{Database: "db", Name: "two", Definition: "CREATE MATERIALIZED VIEW two TO dst AS SELECT * FROM other"}
	result := StaticFlowDiagnostics(view, []View{view, other})
	if result.Flow.Trigger != "db.src" || result.Target == nil || result.Target.Table != "db.dst" || len(result.Writers) != 2 {
		t.Fatalf("unexpected static flow: %+v", result)
	}
}

func TestFlowDiagnosticsQueriesWithClickHouseLocal(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse local unavailable")
	}
	setup := `CREATE TABLE parts (database String,table String,event_date Date,event_time DateTime,event_type String,error UInt16,rows UInt64) ENGINE=Memory;
 CREATE TABLE views (view_name String,event_date Date,event_time DateTime,view_type String,status String) ENGINE=Memory;
 INSERT INTO parts VALUES ('db','src',today(),now(),'NewPart',0,12),('db','src',today(),now(),'DownloadPart',0,9),('db','src',today(),now(),'NewPart',1,9),('db','other',today(),now(),'NewPart',0,9);
 INSERT INTO parts SELECT 'db','src',today(),now()-INTERVAL 6 MINUTE,'NewPart',0,100;
 INSERT INTO parts SELECT 'db','src',today(),now()+INTERVAL 1 MINUTE,'NewPart',0,100;
 INSERT INTO views SELECT 'db.mv',today(),now(),'Materialized',arrayJoin(['QueryStart','QueryFinish','ExceptionWhileProcessing']);
 INSERT INTO views SELECT 'db.mv',today(),now()-INTERVAL 6 MINUTE,'Materialized','ExceptionBeforeStart';
 INSERT INTO views SELECT 'db.mv',today(),now()+INTERVAL 1 MINUTE,'Materialized','ExceptionBeforeStart';`
	partQuery := strings.ReplaceAll(flowPartDiagnosticsQuery("'db.src'"), "system.part_log", "parts")
	viewQuery := strings.ReplaceAll(flowViewDiagnosticsQuery(), "system.query_views_log", "views")
	viewQuery = strings.Replace(viewQuery, "view_name = ?", "view_name = 'db.mv'", 1)
	output, err := exec.Command(binary, "local", "--multiquery", "--query", setup+partQuery+";"+viewQuery).CombinedOutput()
	if err != nil {
		t.Fatalf("queries failed: %v: %s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 || lines[0] != "db\tsrc\t1\t12" || !strings.HasPrefix(lines[1], "1\t1\t") {
		t.Fatalf("unexpected results: %s", output)
	}
}
