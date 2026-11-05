package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type PartObservation struct {
	Table        string
	Events, Rows uint64
	State        MetricState
	Detail       string
}
type FlowDiagnostics struct {
	Flow                     Flow
	Sources                  []PartObservation
	Target                   *PartObservation
	Writers                  []string
	Successes, Failures      uint64
	LastSuccess, LastFailure time.Time
	ViewError                string
	PartError                string
	Offline                  bool
}

// LoadFlowDiagnostics collects independent local-node observations for five minutes.
func LoadFlowDiagnostics(ctx context.Context, dsn string, view View, views []View) FlowDiagnostics {
	data := StaticFlowDiagnostics(view, views)
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		data.ViewError = RedactConnectionError(err, dsn).Error()
		data.PartError = data.ViewError
		return data
	}
	defer db.Close()
	data = collectFlowDiagnostics(ctx, db, data, view)
	redact := func(s string) string {
		if s == "" {
			return ""
		}
		return RedactConnectionError(fmt.Errorf("%s", s), dsn).Error()
	}
	data.ViewError, data.PartError = redact(data.ViewError), redact(data.PartError)
	for i := range data.Sources {
		data.Sources[i].Detail = redact(data.Sources[i].Detail)
	}
	if data.Target != nil {
		data.Target.Detail = redact(data.Target.Detail)
	}
	return data
}
func StaticFlowDiagnostics(view View, views []View) FlowDiagnostics {
	data := FlowDiagnostics{Flow: InferFlow(view)}
	for _, s := range data.Flow.Sources {
		data.Sources = append(data.Sources, PartObservation{Table: s, State: MetricUnknown})
	}
	target := data.Flow.Target
	if target != "" && target != "unknown target" && target != "internal storage (no TO)" {
		data.Target = &PartObservation{Table: target, State: MetricUnknown}
		for _, other := range views {
			if InferFlow(other).Target == target {
				data.Writers = append(data.Writers, other.Database+"."+other.Name)
			}
		}
	}
	return data
}
func collectFlowDiagnostics(ctx context.Context, db *sql.DB, data FlowDiagnostics, view View) FlowDiagnostics {
	var names []string
	for _, s := range data.Sources {
		names = append(names, s.Table)
	}
	if data.Target != nil {
		names = append(names, data.Target.Table)
	}
	diagnostics, err := tableMetricDiagnostics(ctx, db, names)
	if err != nil {
		data.PartError = "table metadata: " + err.Error()
	}
	observations := map[string]PartObservation{}
	for _, name := range names {
		d, ok := diagnostics[name]
		if !ok {
			d.State = MetricUnknown
		}
		observations[name] = PartObservation{Table: name, State: d.State, Detail: d.Detail}
	}
	if len(names) > 0 && err == nil {
		args, marks := queryArgs(names)
		query := flowPartDiagnosticsQuery(marks)
		rows, e := db.QueryContext(ctx, query, args...)
		if e == nil {
			for rows.Next() {
				var database, table string
				var n, count uint64
				if e = rows.Scan(&database, &table, &n, &count); e != nil {
					break
				}
				name := database + "." + table
				o := observations[name]
				if o.State == MetricNoEvents {
					o.Events, o.Rows, o.State = n, count, MetricAvailable
					observations[name] = o
				}
			}
			if e == nil {
				e = rows.Err()
			}
			rows.Close()
		}
		if e != nil {
			data.PartError = "part log: " + e.Error()
			for name, o := range observations {
				if o.State == MetricNoEvents || o.State == MetricAvailable {
					o.State = MetricLogError
					o.Detail = e.Error()
					observations[name] = o
				}
			}
		}
	}
	for i := range data.Sources {
		data.Sources[i] = observations[data.Sources[i].Table]
	}
	if data.Target != nil {
		value := observations[data.Target.Table]
		data.Target = &value
	}
	query := flowViewDiagnosticsQuery()
	if err := db.QueryRowContext(ctx, query, view.Database+"."+view.Name).Scan(&data.Successes, &data.Failures, &data.LastSuccess, &data.LastFailure); err != nil {
		data.ViewError = "view log: " + err.Error()
	}
	return data
}

func flowPartDiagnosticsQuery(marks string) string {
	return `SELECT database, table, count(), sum(rows) FROM system.part_log WHERE event_date >= today() - 1 AND event_time >= now() - INTERVAL 5 MINUTE AND event_time <= now() AND event_type = 'NewPart' AND error = 0 AND concat(database, '.', table) IN (` + marks + `) GROUP BY database, table`
}

func flowViewDiagnosticsQuery() string {
	return `SELECT countIf(status = 'QueryFinish'), countIf(status IN ('ExceptionBeforeStart','ExceptionWhileProcessing')), maxIf(event_time, status = 'QueryFinish'), maxIf(event_time, status IN ('ExceptionBeforeStart','ExceptionWhileProcessing')) FROM system.query_views_log WHERE event_date >= today() - 1 AND event_time >= now() - INTERVAL 5 MINUTE AND event_time <= now() AND view_type = 'Materialized' AND view_name = ?`
}
