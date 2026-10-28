package clickhouse

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type failingBatchDriver struct{}
type failingBatchConn struct{}
type metadataRows struct {
	values [][]driver.Value
	index  int
}

func (failingBatchDriver) Open(string) (driver.Conn, error) { return failingBatchConn{}, nil }
func (failingBatchConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (failingBatchConn) Close() error              { return nil }
func (failingBatchConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (failingBatchConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "FROM system.tables") {
		rows := &metadataRows{}
		for _, arg := range args {
			parts := strings.SplitN(arg.Value.(string), ".", 2)
			rows.values = append(rows.values, []driver.Value{parts[0], parts[1], "MergeTree"})
		}
		return rows, nil
	}
	return nil, errors.New("test log unavailable")
}
func (*metadataRows) Columns() []string { return []string{"database", "name", "engine"} }
func (*metadataRows) Close() error      { return nil }
func (r *metadataRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
func init() { sql.Register("chinspector_failing_batch", failingBatchDriver{}) }
func TestOverviewLeavesUnqueriedBatchesUnknown(t *testing.T) {
	db, err := sql.Open("chinspector_failing_batch", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	views := make([]View, 101)
	for i := range views {
		views[i] = View{Database: "db", Name: fmt.Sprintf("mv_%03d", i), Definition: fmt.Sprintf("CREATE MATERIALIZED VIEW mv TO dst AS SELECT * FROM src_%03d", i)}
	}
	got := collectOverviewRates(context.Background(), db, "test-batch-no-credentials", views, 60, 24)
	if got.TableDiagnostics["db.src_000"].State != MetricLogError {
		t.Fatalf("failed batch: %#v", got.TableDiagnostics["db.src_000"])
	}
	if got.TableDiagnostics["db.src_100"].State != MetricUnknown {
		t.Fatalf("unqueried batch was misreported: %#v", got.TableDiagnostics["db.src_100"])
	}
	if got.ViewDiagnostics["db.mv_100"].State != MetricUnknown {
		t.Fatalf("unqueried view was misreported: %#v", got.ViewDiagnostics["db.mv_100"])
	}
}
