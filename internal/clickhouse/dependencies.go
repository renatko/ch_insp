package clickhouse

import "sort"

type TableDependency struct {
	View                   View
	Direction, Role, Other string
}

// TableDependencies describes inferred relationships in the visible catalog.
// The graph may be incomplete when permissions or SQL inference are incomplete.
func TableDependencies(table string, views []View) []TableDependency {
	var result []TableDependency
	for _, view := range views {
		flow := InferFlow(view)
		if flow.Target == table {
			result = append(result, TableDependency{View: view, Direction: "writes", Other: table})
		}
		for _, source := range flow.Sources {
			if source != table {
				continue
			}
			role := "input; trigger unknown"
			if flow.Trigger != "" {
				if source == flow.Trigger {
					role = "inferred trigger"
				} else {
					role = "other query input"
				}
			}
			result = append(result, TableDependency{View: view, Direction: "reads", Role: role, Other: flow.Target})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Direction != b.Direction {
			return a.Direction < b.Direction
		}
		if a.View.Database != b.View.Database {
			return a.View.Database < b.View.Database
		}
		return a.View.Name < b.View.Name
	})
	return result
}
