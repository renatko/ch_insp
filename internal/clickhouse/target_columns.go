package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// TargetColumns describes potential MV outputs, not a record of actual writes.
type TargetColumns struct {
	Database, Table string
	Columns         []TargetColumn
	Views           []string
	Warnings        []string
}
type TargetColumn struct {
	Name, Type, DefaultKind, DefaultExpression string
	Writers                                    []TargetWriter
}
type TargetWriter struct {
	View, Expression, ViewType string
	Inputs                     []string
}

// LoadTargetColumns discovers all visible materialized views targeting the
// selected view's table, including views outside the current UI filter/database.
func LoadTargetColumns(ctx context.Context, dsn string, selected View, catalog []View) (TargetColumns, error) {
	plan, err := parseLineage(selected)
	if err != nil {
		return TargetColumns{}, fmt.Errorf("identify target of %s: %w", selected.Name, err)
	}
	if plan.target == nil {
		return TargetColumns{}, fmt.Errorf("%s has internal storage and no explicit target table", selected.Name)
	}
	target := *plan.target
	result := TargetColumns{Database: target.Database, Table: target.Name}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return result, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT name, type, default_kind, default_expression FROM system.columns WHERE database = ? AND table = ? ORDER BY position`, target.Database, target.Name)
	if err != nil {
		return result, fmt.Errorf("columns for %s: %w", target.key(), err)
	}
	for rows.Next() {
		var c TargetColumn
		if err = rows.Scan(&c.Name, &c.Type, &c.DefaultKind, &c.DefaultExpression); err != nil {
			break
		}
		result.Columns = append(result.Columns, c)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return result, fmt.Errorf("read columns for %s: %w", target.key(), err)
	}
	if len(result.Columns) == 0 {
		return result, fmt.Errorf("no visible column metadata for %s (missing object or insufficient access)", target.key())
	}
	for _, view := range catalog {
		if ctx.Err() != nil {
			result.Warnings = append(result.Warnings, "Analysis stopped: "+ctx.Err().Error())
			break
		}
		// The flow fallback identifies a likely target even if the SQL parser fails.
		candidate, parseErr := parseLineage(view)
		if parseErr != nil {
			if InferFlow(view).Target == target.key() {
				result.Views = append(result.Views, view.Database+"."+view.Name)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s.%s: target and column analysis incomplete: %v", view.Database, view.Name, parseErr))
			continue
		}
		if candidate.target == nil || candidate.target.Database != target.Database || candidate.target.Name != target.Name {
			continue
		}
		name := view.Database + "." + view.Name
		result.Views = append(result.Views, name)
		lineage, loadErr := LoadColumnLineage(ctx, dsn, view)
		if loadErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", name, loadErr))
			continue
		}
		addTargetWriters(&result, name, lineage)
	}
	return result, nil
}

func addTargetWriters(target *TargetColumns, view string, lineage ColumnLineage) {
	for _, warning := range lineage.Warnings {
		target.Warnings = append(target.Warnings, view+": "+warning)
	}
	for _, mapping := range lineage.Columns {
		if mapping.TargetStatus != "matched by name" {
			if strings.Contains(mapping.TargetStatus, "unresolved") || (mapping.TargetStatus == "target metadata unavailable" || mapping.TargetStatus == "not found in MV schema; inspect SQL") {
				target.Warnings = append(target.Warnings, fmt.Sprintf("%s: %s (%s)", view, mapping.Output, mapping.TargetStatus))
			}
			continue
		}
		for i := range target.Columns {
			if target.Columns[i].Name == mapping.Output {
				target.Columns[i].Writers = append(target.Columns[i].Writers, TargetWriter{View: view, Expression: mapping.Expression, ViewType: mapping.ViewType, Inputs: append([]string(nil), mapping.Inputs...)})
				break
			}
		}
	}
}
