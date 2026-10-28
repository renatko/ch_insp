package clickhouse

import (
	"context"
	"database/sql"
	"strings"
)

type ViewRate struct{ Read, Written float64 }
type OverviewRates struct {
	Tables       map[string]float64
	TableHistory map[string][5]uint64
	Views        map[string]ViewRate
	History      map[string]ViewHistory
	Errors       []string
}

// LoadOverviewRates reads the same local-node, 60-second log window as LoadRates,
// grouped for all objects in the selected database's flows.
func LoadOverviewRates(ctx context.Context, dsn string, views []View) OverviewRates {
	out := OverviewRates{Tables: map[string]float64{}, Views: map[string]ViewRate{}}
	if len(views) == 0 {
		return out
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		out.Errors = append(out.Errors, "open metrics connection: "+err.Error())
		return out
	}
	defer db.Close()
	var tableNames, viewNames []string
	seen := map[string]bool{}
	for _, view := range views {
		viewNames = append(viewNames, view.Database+"."+view.Name)
		flow := InferFlow(view)
		for _, name := range append(flow.Sources, flow.Target) {
			if strings.Count(name, ".") != 1 || seen[name] {
				continue
			}
			seen[name] = true
			tableNames = append(tableNames, name)
		}
	}
	for start := 0; start < len(tableNames); start += 100 {
		end := start + 100
		if end > len(tableNames) {
			end = len(tableNames)
		}
		args, marks := queryArgs(tableNames[start:end])
		rows, err := db.QueryContext(ctx, `SELECT database, table, sum(rows) FROM system.part_log
			WHERE event_date >= today() - 1 AND event_time > now() - INTERVAL 60 SECOND
			AND event_type = 'NewPart' AND error = 0 AND concat(database, '.', table) IN (`+marks+`)
			GROUP BY database, table`, args...)
		if err != nil {
			out.Errors = append(out.Errors, "part log: "+err.Error())
			break
		}
		for rows.Next() {
			var database, table string
			var count uint64
			if err = rows.Scan(&database, &table, &count); err != nil {
				break
			}
			out.Tables[database+"."+table] = float64(count) / 60
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			out.Errors = append(out.Errors, "part log: "+err.Error())
			break
		}
	}
	for start := 0; start < len(viewNames); start += 100 {
		end := start + 100
		if end > len(viewNames) {
			end = len(viewNames)
		}
		args, marks := queryArgs(viewNames[start:end])
		rows, err := db.QueryContext(ctx, `SELECT view_name, sum(read_rows), sum(written_rows) FROM system.query_views_log
			WHERE event_date >= today() - 1 AND event_time > now() - INTERVAL 60 SECOND
			AND view_type = 'Materialized' AND status = 'QueryFinish' AND view_name IN (`+marks+`)
			GROUP BY view_name`, args...)
		if err != nil {
			out.Errors = append(out.Errors, "view log: "+err.Error())
			break
		}
		for rows.Next() {
			var name string
			var read, written uint64
			if err = rows.Scan(&name, &read, &written); err != nil {
				break
			}
			out.Views[name] = ViewRate{Read: float64(read) / 60, Written: float64(written) / 60}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			out.Errors = append(out.Errors, "view log: "+err.Error())
			break
		}
	}
	tableHistory, tableHistoryErr := loadTableHistories(ctx, db, tableNames)
	out.TableHistory = tableHistory
	if tableHistoryErr != nil {
		out.Errors = append(out.Errors, "part history: "+tableHistoryErr.Error())
	}
	history, historyErr := loadViewHistories(ctx, db, viewNames)
	out.History = history
	if historyErr != nil {
		out.Errors = append(out.Errors, "view history: "+historyErr.Error())
	}
	return out
}

func queryArgs(names []string) ([]any, string) {
	args := make([]any, len(names))
	for i, name := range names {
		args[i] = name
	}
	return args, strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
}
