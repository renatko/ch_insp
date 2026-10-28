package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	parser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// EXPLAIN can evaluate constants and access table functions while analyzing a
// query. Restrict this first implementation to plain identifiers, operators,
// and physical sources; leave all other queries to the static analyzer.
func explainEligible(plan lineagePlan) bool {
	if plan.scope == nil || len(plan.sources) == 0 {
		return false
	}
	valid := true
	parser.Walk(plan.query, func(n parser.Expr) bool {
		switch n.(type) {
		case *parser.FunctionExpr, *parser.SubQuery:
			// CTE and FROM subqueries are traversed by the scope below; scalar
			// subqueries and function calls are excluded to avoid side effects.
			if _, ok := n.(*parser.FunctionExpr); ok {
				valid = false
			}
		}
		return true
	})
	var inspect func(*lineageScope)
	inspect = func(s *lineageScope) {
		if s == nil || s.incomplete || len(s.scalars) > 0 || s.query.Settings != nil || s.query.Format != nil {
			valid = false
			return
		}
		for _, r := range s.relations {
			if r.derived != nil {
				inspect(r.derived)
			} else if r.table == nil {
				valid = false
			}
		}
	}
	inspect(plan.scope)
	return valid
}

type explainNode struct {
	text     string
	children []*explainNode
}

var explainID = regexp.MustCompile(`\bid: ([0-9]+)(?:,|$)`)
var explainSource = regexp.MustCompile(`(?:^|, )source_id: ([0-9]+)(?:,|$)`)

func explainField(s, key string) string {
	marker := key + ": "
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	v := s[i+len(marker):]
	if j := strings.Index(v, ", "); j >= 0 {
		v = v[:j]
	}
	return v
}
func explainTree(lines []string) *explainNode {
	root := &explainNode{}
	stack := []*explainNode{root}
	depths := []int{-1}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		depth := len(line) - len(strings.TrimLeft(line, " "))
		node := &explainNode{text: strings.TrimSpace(line)}
		for len(depths) > 1 && depths[len(depths)-1] >= depth {
			stack = stack[:len(stack)-1]
			depths = depths[:len(depths)-1]
		}
		parent := stack[len(stack)-1]
		parent.children = append(parent.children, node)
		stack = append(stack, node)
		depths = append(depths, depth)
	}
	return root
}
func explainChild(n *explainNode, label string) *explainNode {
	for _, c := range n.children {
		if c.text == label {
			return c
		}
	}
	return nil
}
func explainProjection(q *explainNode) ([]string, []*explainNode) {
	cols, proj := explainChild(q, "PROJECTION COLUMNS"), explainChild(q, "PROJECTION")
	if cols == nil || proj == nil || len(proj.children) != 1 {
		return nil, nil
	}
	var names []string
	for _, col := range cols.children {
		i := strings.IndexByte(col.text, ' ')
		if i < 1 {
			return nil, nil
		}
		names = append(names, col.text[:i])
	}
	exprs := proj.children[0].children
	if len(names) != len(exprs) {
		return nil, nil
	}
	return names, exprs
}
func resolvedExplain(lines []string, mappings []ColumnMapping, metadata map[string][]tableColumn, sources []lineageTable) map[int][]string {
	root := explainTree(lines)
	if len(root.children) != 1 || !strings.HasPrefix(root.children[0].text, "QUERY id: ") {
		return nil
	}
	ids := map[string]*explainNode{}
	var index func(*explainNode)
	index = func(n *explainNode) {
		if m := explainID.FindStringSubmatch(n.text); len(m) > 1 {
			ids[m[1]] = n
		}
		for _, c := range n.children {
			index(c)
		}
	}
	index(root)
	var resolve func(*explainNode, map[string]bool) ([]string, bool)
	resolve = func(n *explainNode, visiting map[string]bool) ([]string, bool) {
		if strings.HasPrefix(n.text, "COLUMN id: ") {
			source := explainSource.FindStringSubmatch(n.text)
			name := explainField(n.text, "column_name")
			if len(source) < 2 || name == "" {
				return nil, false
			}
			id := source[1]
			if visiting[id+":"+name] {
				return nil, false
			}
			src := ids[id]
			if src == nil {
				return nil, false
			}
			if strings.HasPrefix(src.text, "TABLE id: ") {
				tableName := explainField(src.text, "table_name")
				matches := 0
				for _, t := range sources {
					if t.key() == tableName {
						matches++
					}
				}
				if matches != 1 {
					return nil, false
				}
				for _, t := range sources {
					if t.key() == tableName {
						for _, c := range metadata[t.key()] {
							if c.Name == name {
								return []string{displaySourceColumn(t, c, sources)}, true
							}
						}
					}
				}
				return nil, false
			}
			if strings.HasPrefix(src.text, "QUERY id: ") {
				names, exprs := explainProjection(src)
				found := -1
				for i, v := range names {
					if v == name {
						if found >= 0 {
							return nil, false
						}
						found = i
					}
				}
				if found < 0 {
					return nil, false
				}
				key := id + ":" + name
				visiting[key] = true
				inputs, ok := resolve(exprs[found], visiting)
				delete(visiting, key)
				return inputs, ok
			}
			return nil, false
		}
		if strings.HasPrefix(n.text, "CONSTANT id: ") {
			return nil, true
		}
		if !strings.HasPrefix(n.text, "FUNCTION id: ") && !strings.HasPrefix(n.text, "LIST id: ") && n.text != "ARGUMENTS" {
			return nil, false
		}
		var out []string
		seen := map[string]bool{}
		for _, c := range n.children {
			values, ok := resolve(c, visiting)
			if !ok {
				return nil, false
			}
			for _, v := range values {
				if !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
		return out, true
	}
	names, exprs := explainProjection(root.children[0])
	if len(names) != len(mappings) {
		return nil
	}
	out := map[int][]string{}
	for i, name := range names {
		if name != mappings[i].Output {
			continue
		}
		values, ok := resolve(exprs[i], map[string]bool{})
		if ok && len(values) > 0 {
			out[i] = values
		}
	}
	return out
}

func enrichWithExplain(ctx context.Context, db *sql.DB, view View, plan lineagePlan, metadata map[string][]tableColumn, out *ColumnLineage) {
	if !explainEligible(plan) {
		return
	}
	// Keep resolution in the same database context as the stored definition.
	var current string
	if err := db.QueryRowContext(ctx, "SELECT currentDatabase()").Scan(&current); err != nil || current != view.Database {
		return
	}
	for _, t := range plan.sources {
		var engine string
		if err := db.QueryRowContext(ctx, "SELECT engine FROM system.tables WHERE database = ? AND name = ?", t.Database, t.Name).Scan(&engine); err != nil {
			return
		}
		// Avoid remote, dictionary, executable and other potentially external engines.
		if engine != "Memory" && engine != "Null" && engine != "Log" && engine != "TinyLog" && engine != "StripeLog" && !strings.HasSuffix(engine, "MergeTree") {
			return
		}
	}
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY TREE "+plan.query.String())
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("ClickHouse query analysis: %v", err))
		return
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			break
		}
		lines = append(lines, line)
		if len(lines) > 100000 {
			err = fmt.Errorf("query tree exceeds size limit")
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("ClickHouse query analysis: %v", err))
		return
	}
	for i, inputs := range resolvedExplain(lines, out.Columns, metadata, plan.sources) {
		for _, old := range out.Columns[i].Inputs {
			if strings.Contains(old, "ambiguous") || strings.Contains(old, "unresolved") {
				out.Columns[i].Inputs = inputs
				break
			}
		}
	}
}
