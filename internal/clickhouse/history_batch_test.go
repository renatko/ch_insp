package clickhouse

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"
)

type historyFixtureDriver struct{}
type historyFixtureConn struct{ mode string }
type historyFixtureRows struct {
	name    string
	partial bool
	n       int
}

func (historyFixtureDriver) Open(mode string) (driver.Conn, error) {
	return historyFixtureConn{mode}, nil
}
func (historyFixtureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (historyFixtureConn) Close() error { return nil }
func (historyFixtureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c historyFixtureConn) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	return &historyFixtureRows{name: args[0].Value.(string), partial: c.mode == "partial"}, nil
}
func (*historyFixtureRows) Columns() []string {
	names := make([]string, 16)
	for i := range names {
		names[i] = "field"
	}
	return names
}
func (*historyFixtureRows) Close() error { return nil }
func (r *historyFixtureRows) Next(dest []driver.Value) error {
	if !r.partial {
		return io.EOF
	}
	if r.n > 0 {
		return errors.New("interrupted")
	}
	r.n++
	dest[0] = r.name
	for i := 1; i < len(dest); i++ {
		dest[i] = int64(1)
	}
	return nil
}
func init() { sql.Register("chinspector_history_fixture", historyFixtureDriver{}) }
func TestHistoryOnlyMarksSuccessfullyReadEmptyBatches(t *testing.T) {
	end := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	names := []string{"db.one", "db.two"}
	db, err := sql.Open("chinspector_history_fixture", "empty")
	if err != nil {
		t.Fatal(err)
	}
	histories, err := loadViewHistories(context.Background(), db, names, 60, "empty-history-test", end)
	db.Close()
	if err != nil || len(histories) != 2 || histories["db.one"].Events != [5]uint64{} {
		t.Fatalf("successful empty batch: %#v %v", histories, err)
	}
	db, err = sql.Open("chinspector_history_fixture", "partial")
	if err != nil {
		t.Fatal(err)
	}
	histories, err = loadViewHistories(context.Background(), db, names, 60, "partial-history-test", end)
	db.Close()
	if err == nil || len(histories) != 0 {
		t.Fatalf("partial batch was presented as complete: %#v %v", histories, err)
	}
}
