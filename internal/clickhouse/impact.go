package clickhouse

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ImpactItem is one inferred edge in a downstream path, not an observed write.
type ImpactItem struct {
	Depth                           int
	Table, Column                   string
	View                            View
	Role, Usage, Expression, Status string
	Cycle                           bool
}

type ImpactResult struct {
	Items     []ImpactItem
	Warnings  []string
	Truncated bool
}

// ImpactLoader loads one view's lineage. It may use live metadata or an SQL dump.
type ImpactLoader func(context.Context, View) (ColumnLineage, error)

// AnalyzeImpact follows inferred downstream relationships; filters do not apply.
// A column propagates only when an output matches a known target by name.
func AnalyzeImpact(ctx context.Context, table, column string, views []View, load ImpactLoader) ImpactResult {
	const maxItems = 500
	const maxViews = 64
	result := ImpactResult{}
	ordered := append([]View(nil), views...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Database+"."+ordered[i].Name < ordered[j].Database+"."+ordered[j].Name
	})
	type cached struct {
		lineage ColumnLineage
		err     error
	}
	cache := map[string]cached{}
	warnings := map[string]bool{}
	warn := func(text string) {
		if !warnings[text] {
			warnings[text] = true
			result.Warnings = append(result.Warnings, text)
		}
	}
	add := func(item ImpactItem) bool {
		if len(result.Items) >= maxItems {
			result.Truncated = true
			return false
		}
		result.Items = append(result.Items, item)
		return true
	}
	readers := make(map[string][]View)
	for _, view := range ordered {
		flow := InferFlow(view)
		if len(flow.Sources) == 0 {
			warn(view.Database + "." + view.Name + ": no source could be inferred; inspect the definition")
		}
		for _, source := range flow.Sources {
			readers[source] = append(readers[source], view)
		}
	}
	var walk func(string, string, int, map[string]bool)
	walk = func(current, col string, depth int, path map[string]bool) {
		if ctx.Err() != nil {
			warn("Analysis interrupted: " + ctx.Err().Error())
			result.Truncated = true
			return
		}
		if result.Truncated {
			return
		}
		key := current + "\x00" + col
		path[key] = true
		defer delete(path, key)
		for _, view := range readers[current] {
			if ctx.Err() != nil {
				warn("Analysis interrupted: " + ctx.Err().Error())
				result.Truncated = true
				return
			}
			if result.Truncated {
				return
			}
			flow := InferFlow(view)
			role := "input; trigger unknown"
			if flow.Trigger == current {
				role = "inferred trigger"
			} else if flow.Trigger != "" {
				role = "other query input"
			}
			base := ImpactItem{Depth: depth, Table: current, Column: col, View: view, Role: role, Status: "inferred"}
			target := flow.Target
			validTarget := target != "" && target != "unknown target" && target != "internal storage (no TO)"
			if col == "" {
				if !add(base) {
					return
				}
				if !validTarget {
					warn(view.Database + "." + view.Name + ": target unavailable or internal storage")
					continue
				}
				targetKey := target + "\x00"
				edge := ImpactItem{Depth: depth + 1, Table: target, View: view, Status: "target", Cycle: path[targetKey]}
				if !add(edge) {
					return
				}
				if !edge.Cycle {
					walk(target, "", depth+2, path)
				}
				continue
			}
			name := view.Database + "." + view.Name
			data, ok := cache[name]
			if !ok {
				if len(cache) >= maxViews {
					warn("Column analysis limit reached (64 views).")
					result.Truncated = true
					return
				}
				data.lineage, data.err = load(ctx, view)
				cache[name] = data
			}
			if data.err != nil {
				base.Status = "unresolved"
				base.Usage = "lineage unavailable"
				add(base)
				warn(name + ": " + data.err.Error())
				continue
			}
			uncertain := false
			for _, warning := range data.lineage.Warnings {
				warn(name + ": " + warning)
				if !strings.HasPrefix(warning, "Offline SQL dump:") && !strings.HasPrefix(warning, "Alias collisions resolved using current connection setting") {
					uncertain = true
				}
			}
			for _, mapping := range data.lineage.Columns {
				matched, unknown := inputMatches(mapping.Inputs, current, col)
				uncertain = uncertain || unknown
				if !matched {
					continue
				}
				edge := base
				edge.Usage, edge.Expression = "SELECT", mapping.Expression
				if mapping.TargetStatus != "matched by name" || !validTarget {
					edge.Status = "unresolved"
					edge.Usage = "SELECT → " + mapping.TargetStatus
					add(edge)
					continue
				}
				if !add(edge) {
					return
				}
				targetKey := target + "\x00" + mapping.Output
				next := ImpactItem{Depth: depth + 1, Table: target, Column: mapping.Output, View: view, Status: "target", Cycle: path[targetKey]}
				if !add(next) {
					return
				}
				if !next.Cycle {
					walk(target, mapping.Output, depth+2, path)
				}
			}
			for _, condition := range data.lineage.Conditions {
				matched, unknown := inputMatches(condition.Inputs, current, col)
				uncertain = uncertain || unknown
				if matched {
					edge := base
					edge.Usage, edge.Expression = condition.Clause, condition.Expression
					if !add(edge) {
						return
					}
					if validTarget {
						targetKey := target + "\x00"
						next := ImpactItem{Depth: depth + 1, Table: target, View: view, Status: "target", Usage: "potential result", Cycle: path[targetKey]}
						if !add(next) {
							return
						}
						if !next.Cycle {
							walk(target, "", depth+2, path)
						}
					}
				}
			}
			if uncertain {
				edge := base
				edge.Status, edge.Usage = "possible", "unresolved SQL or metadata; inspect definition"
				add(edge)
			}
		}
	}
	if column != "" && load == nil {
		result.Warnings = append(result.Warnings, "Column analysis unavailable.")
		return result
	}
	walk(table, column, 0, map[string]bool{})
	if result.Truncated {
		warn(fmt.Sprintf("Results may be incomplete (maximum %d rows or interrupted analysis).", maxItems))
	}
	return result
}

// Existing lineage labels include a short table name, a type and optional
// database disambiguation. Unresolved references are never treated as certain.
func inputMatches(inputs []string, table, column string) (matched, uncertain bool) {
	db, short, ok := strings.Cut(table, ".")
	if !ok {
		return false, true
	}
	prefix := short + "." + column + " ("
	for _, input := range inputs {
		if strings.Contains(input, "(unresolved") || strings.Contains(input, "(ambiguous") || strings.Contains(input, "unresolved:") || strings.Contains(input, "ambiguous") {
			uncertain = true
			continue
		}
		first, _, _ := strings.Cut(input, " → ")
		if !strings.HasPrefix(first, prefix) {
			continue
		}
		if at := strings.Index(first, " [database: "); at >= 0 && !strings.Contains(first[at:], "[database: "+db+"]") {
			continue
		}
		matched = true
	}
	return matched, uncertain
}
