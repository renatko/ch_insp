package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	parser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// ColumnLineage is a best-effort static description of a view's SELECT.
type ColumnLineage struct {
	Columns         []ColumnMapping
	AdditionalReads []string
	TargetExtras    []string
	Warnings        []string
}

type ColumnMapping struct {
	Output, Expression   string
	Inputs               []string
	ViewType, TargetType string
	TargetStatus         string
}

type tableColumn struct{ Name, Type, DefaultKind, DefaultExpression string }
type lineageTable struct{ Database, Name, Alias string }

func (t lineageTable) key() string { return t.Database + "." + t.Name }

type lineagePlan struct {
	query    *parser.SelectQuery
	sources  []lineageTable
	target   *lineageTable
	complex  bool
	warnings []string
}

func parseLineage(view View) (plan lineagePlan, err error) {
	defer func() {
		if recover() != nil {
			plan = lineagePlan{}
			err = fmt.Errorf("SQL parser could not process this definition")
		}
	}()
	statements, err := parser.NewParser(view.Definition).ParseStmts()
	if err != nil {
		return plan, fmt.Errorf("parse CREATE MATERIALIZED VIEW: %w", err)
	}
	if len(statements) != 1 {
		return plan, fmt.Errorf("expected one CREATE MATERIALIZED VIEW statement")
	}
	mv, ok := statements[0].(*parser.CreateMaterializedView)
	if !ok || mv.SubQuery == nil || mv.SubQuery.Select == nil {
		return plan, fmt.Errorf("CREATE MATERIALIZED VIEW has no supported SELECT")
	}
	plan.query = mv.SubQuery.Select
	if mv.Destination != nil && mv.Destination.TableIdentifier != nil {
		t := tableFromIdentifier(mv.Destination.TableIdentifier, view.Database)
		plan.target = &t
	}
	if plan.query.With != nil || plan.query.UnionAll != nil || plan.query.UnionDistinct != nil || plan.query.Except != nil || plan.query.ArrayJoin != nil {
		plan.complex = true
		plan.warnings = append(plan.warnings, "WITH, UNION, EXCEPT or ARRAY JOIN requires manual lineage inspection; references are not resolved to physical tables.")
	}
	for _, item := range plan.query.SelectItems {
		hasSubquery := false
		hasLambda := false
		parser.Walk(item.Expr, func(n parser.Expr) bool {
			switch expr := n.(type) {
			case *parser.SubQuery, *parser.SelectQuery:
				hasSubquery = true
			case *parser.BinaryOperation:
				if expr.Operation == parser.TokenKindArrow {
					hasLambda = true
				}
			}
			return true
		})
		if hasSubquery {
			plan.complex = true
			plan.warnings = append(plan.warnings, "A SELECT expression contains a subquery; source attribution requires manual inspection.")
			break
		}
		if hasLambda {
			plan.complex = true
			plan.warnings = append(plan.warnings, "Lambda expressions require manual lineage inspection; parameter names are not resolved to physical columns.")
			break
		}
	}
	var collect func(parser.Expr)
	collect = func(expr parser.Expr) {
		switch node := expr.(type) {
		case *parser.JoinExpr:
			if node != nil {
				collect(node.Left)
				collect(node.Right)
			}
		case *parser.JoinTableExpr:
			if node != nil {
				collect(node.Table)
			}
		case *parser.TableExpr:
			if node == nil {
				return
			}
			var alias string
			value := node.Expr
			if a, ok := value.(*parser.AliasExpr); ok {
				value = a.Expr
				if id, ok := a.Alias.(*parser.Ident); ok {
					alias = id.Name
				}
			}
			if node.Alias != nil {
				if id, ok := node.Alias.Alias.(*parser.Ident); ok {
					alias = id.Name
				}
			}
			if id, ok := value.(*parser.TableIdentifier); ok && id.Table != nil {
				t := tableFromIdentifier(id, view.Database)
				t.Alias = alias
				plan.sources = append(plan.sources, t)
			} else {
				plan.complex = true
				plan.warnings = append(plan.warnings, "A subquery, table function or unsupported FROM source requires manual lineage inspection.")
			}
		case nil:
		default:
			plan.complex = true
			plan.warnings = append(plan.warnings, "Unsupported FROM structure; source attribution may be incomplete.")
		}
	}
	if plan.query.From != nil {
		collect(plan.query.From.Expr)
	}
	return plan, nil
}

func tableFromIdentifier(id *parser.TableIdentifier, fallback string) lineageTable {
	database := fallback
	if id.Database != nil {
		database = id.Database.Name
	}
	return lineageTable{Database: database, Name: id.Table.Name}
}

// LoadColumnLineage only reads system.columns; it never executes the view SELECT.
func LoadColumnLineage(ctx context.Context, dsn string, view View) (ColumnLineage, error) {
	plan, err := parseLineage(view)
	if err != nil {
		return ColumnLineage{}, err
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return analyzeColumns(view, plan, nil, []string{"Metadata connection: " + err.Error()}), nil
	}
	defer db.Close()
	metadata := map[string][]tableColumn{}
	var warnings []string
	tables := append([]lineageTable(nil), plan.sources...)
	tables = append(tables, lineageTable{Database: view.Database, Name: view.Name})
	if plan.target != nil {
		tables = append(tables, *plan.target)
	}
	seen := map[string]bool{}
	for _, table := range tables {
		if seen[table.key()] {
			continue
		}
		seen[table.key()] = true
		rows, err := db.QueryContext(ctx, `SELECT name, type, default_kind, default_expression FROM system.columns WHERE database = ? AND table = ? ORDER BY position`, table.Database, table.Name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Columns for %s: %v", table.key(), err))
			continue
		}
		var cols []tableColumn
		for rows.Next() {
			var c tableColumn
			if err = rows.Scan(&c.Name, &c.Type, &c.DefaultKind, &c.DefaultExpression); err != nil {
				break
			}
			cols = append(cols, c)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Columns for %s: %v", table.key(), err))
			continue
		}
		if len(cols) == 0 {
			warnings = append(warnings, fmt.Sprintf("No visible column metadata for %s (missing object or insufficient access).", table.key()))
			continue
		}
		metadata[table.key()] = cols
	}
	return analyzeColumns(view, plan, metadata, warnings), nil
}

type sqlReference struct{ qualifier, name string }

// references records identifier nodes while excluding function names, aliases,
// type names and the components of qualified paths.
func references(expr parser.Expr) []sqlReference {
	ignored := map[*parser.Ident]bool{}
	var identifiers []*parser.Ident
	var refs []sqlReference
	parser.Walk(expr, func(n parser.Expr) bool {
		switch v := n.(type) {
		case *parser.FunctionExpr:
			ignored[v.Name] = true
		case *parser.ColumnExpr:
			if v.Alias != nil {
				ignored[v.Alias] = true
			}
		case *parser.CastExpr:
			parser.Walk(v.AsType, func(x parser.Expr) bool {
				if id, ok := x.(*parser.Ident); ok {
					ignored[id] = true
				}
				return true
			})
		case *parser.Path:
			for _, id := range v.Fields {
				ignored[id] = true
			}
			if len(v.Fields) > 0 {
				parts := make([]string, 0, len(v.Fields))
				for _, id := range v.Fields {
					parts = append(parts, id.Name)
				}
				refs = append(refs, sqlReference{qualifier: strings.Join(parts[:len(parts)-1], "."), name: parts[len(parts)-1]})
			}
		case *parser.NestedIdentifier:
			ignored[v.Ident] = true
			if v.DotIdent != nil {
				ignored[v.DotIdent] = true
				refs = append(refs, sqlReference{qualifier: v.Ident.Name, name: v.DotIdent.Name})
			} else {
				refs = append(refs, sqlReference{name: v.Ident.Name})
			}
		case *parser.Ident:
			identifiers = append(identifiers, v)
		}
		return true
	})
	for _, id := range identifiers {
		if !ignored[id] && id.Name != "*" {
			refs = append(refs, sqlReference{name: id.Name})
		}
	}
	return refs
}

func resolveReferences(refs []sqlReference, plan lineagePlan, metadata map[string][]tableColumn, aliases map[string]bool) []string {
	seen := map[string]bool{}
	var result []string
	for _, ref := range refs {
		if ref.name == "*" {
			continue
		}
		label := ref.name
		if ref.qualifier != "" {
			label = ref.qualifier + "." + ref.name
		}
		var matches []string
		metadataIncomplete := false
		if !plan.complex && !(ref.qualifier == "" && aliases[ref.name]) {
			for _, table := range plan.sources {
				if ref.qualifier != "" && ref.qualifier != table.Alias && ref.qualifier != table.Name && ref.qualifier != table.key() {
					continue
				}
				if _, ok := metadata[table.key()]; !ok {
					metadataIncomplete = true
				}
				for _, col := range metadata[table.key()] {
					if col.Name == ref.name {
						matches = append(matches, displaySourceColumn(table, col, plan.sources))
					}
				}
			}
		}
		var resolved string
		if metadataIncomplete {
			resolved = label + " (unresolved: source metadata incomplete)"
		} else if len(matches) == 1 {
			resolved = matches[0]
		} else if len(matches) > 1 {
			resolved = label + " (ambiguous: " + strings.Join(matches, ", ") + ")"
		} else {
			resolved = label + " (unresolved)"
		}
		if !seen[resolved] {
			seen[resolved] = true
			result = append(result, resolved)
		}
	}
	return result
}

func displaySourceColumn(table lineageTable, col tableColumn, sources []lineageTable) string {
	name := table.Name + "." + col.Name + " (" + col.Type + ")"
	for _, other := range sources {
		if other.Name == table.Name && other.Database != table.Database {
			return name + " [database: " + table.Database + "]"
		}
		if other.Name == table.Name && other.Database == table.Database && other.Alias != table.Alias && table.Alias != "" {
			return name + " [alias: " + table.Alias + "]"
		}
	}
	return name
}

func analyzeColumns(view View, plan lineagePlan, metadata map[string][]tableColumn, warnings []string) ColumnLineage {
	out := ColumnLineage{Warnings: append(append([]string(nil), plan.warnings...), warnings...)}
	aliases := map[string]bool{}
	for _, item := range plan.query.SelectItems {
		if item.Alias != nil {
			aliases[item.Alias.Name] = true
		}
	}
	viewColumns := metadata[view.Database+"."+view.Name]
	var targetColumns []tableColumn
	if plan.target != nil {
		targetColumns = metadata[plan.target.key()]
	}
	seenOutput := map[string]bool{}
	add := func(name, expression string, inputs []string, nameKnown bool) {
		mapping := ColumnMapping{Output: name, Expression: expression, Inputs: inputs}
		for _, col := range viewColumns {
			if col.Name == name {
				mapping.ViewType = col.Type
				nameKnown = true
				break
			}
		}
		knownInMV := len(viewColumns) == 0 || mapping.ViewType != ""
		if !nameKnown {
			mapping.TargetStatus = "output name unresolved; inspect SQL"
		} else if !knownInMV {
			mapping.TargetStatus = "not found in MV schema; inspect SQL"
		} else if plan.target != nil {
			if len(targetColumns) == 0 {
				mapping.TargetStatus = "target metadata unavailable"
			} else {
				mapping.TargetStatus = "no matching target column"
				for _, col := range targetColumns {
					if col.Name == name {
						mapping.TargetType = col.Type
						mapping.TargetStatus = "matched by name"
						break
					}
				}
			}
		} else {
			mapping.TargetStatus = "internal storage"
		}
		if nameKnown && knownInMV {
			seenOutput[name] = true
		}
		out.Columns = append(out.Columns, mapping)
	}
	for _, item := range plan.query.SelectItems {
		expression := item.Expr.String()
		if len(item.Modifiers) > 0 {
			out.Warnings = append(out.Warnings, "SELECT modifiers are not expanded; inspect the definition.")
		}
		if expression == "*" || strings.HasSuffix(expression, ".*") {
			var candidates []lineageTable
			if !plan.complex && len(item.Modifiers) == 0 {
				for _, table := range plan.sources {
					qualifier := strings.TrimSuffix(expression, ".*")
					if expression == "*" || qualifier == table.Alias || qualifier == table.Name || qualifier == table.key() {
						candidates = append(candidates, table)
					}
				}
			}
			if len(candidates) == 1 && len(metadata[candidates[0].key()]) > 0 && len(viewColumns) > 0 {
				expanded := 0
				for _, col := range metadata[candidates[0].key()] {
					for _, output := range viewColumns {
						if output.Name == col.Name {
							add(col.Name, expression, []string{displaySourceColumn(candidates[0], col, plan.sources)}, true)
							expanded++
							break
						}
					}
				}
				if expanded == 0 {
					add("* (unresolved)", expression, []string{"No matching wildcard output columns in view metadata."}, false)
					out.Warnings = append(out.Warnings, "Wildcard expansion is incomplete; inspect SELECT and source schemas.")
				}
			} else {
				add("* (unresolved)", expression, []string{"Wildcard expansion requires unambiguous source and view metadata."}, false)
				out.Warnings = append(out.Warnings, "Wildcard expansion is incomplete; inspect SELECT and source schemas.")
			}
			continue
		}
		output := expression
		nameKnown := false
		if item.Alias != nil {
			output = item.Alias.Name
			nameKnown = true
		} else {
			switch v := item.Expr.(type) {
			case *parser.Ident:
				output = v.Name
				nameKnown = true
			case *parser.Path:
				if len(v.Fields) > 0 {
					output = v.Fields[len(v.Fields)-1].Name
					nameKnown = true
				}
			case *parser.NestedIdentifier:
				if v.DotIdent != nil {
					output = v.DotIdent.Name
				} else {
					output = v.Ident.Name
				}
				nameKnown = true
			}
		}
		itemAliases := make(map[string]bool, len(aliases))
		for k, v := range aliases {
			itemAliases[k] = v
		}
		// A simple projection such as `id AS id` refers to the source column.
		if id, ok := item.Expr.(*parser.Ident); ok && item.Alias != nil && id.Name == item.Alias.Name {
			delete(itemAliases, id.Name)
		}
		add(output, expression, resolveReferences(references(item.Expr), plan, metadata, itemAliases), nameKnown)
	}
	var other []parser.Expr
	if plan.query.Prewhere != nil {
		other = append(other, plan.query.Prewhere)
	}
	if plan.query.Where != nil {
		other = append(other, plan.query.Where)
	}
	if plan.query.GroupBy != nil {
		other = append(other, plan.query.GroupBy)
	}
	if plan.query.Having != nil {
		other = append(other, plan.query.Having)
	}
	if plan.query.OrderBy != nil {
		other = append(other, plan.query.OrderBy)
	}
	if plan.query.Window != nil {
		other = append(other, plan.query.Window)
	}
	if plan.query.LimitBy != nil {
		other = append(other, plan.query.LimitBy)
	}
	if plan.query.ArrayJoin != nil {
		other = append(other, plan.query.ArrayJoin)
	}
	if plan.query.From != nil {
		parser.Walk(plan.query.From.Expr, func(n parser.Expr) bool {
			switch clause := n.(type) {
			case *parser.JoinConstraintClause:
				other = append(other, clause)
			case *parser.OnClause:
				other = append(other, clause)
			case *parser.UsingClause:
				other = append(other, clause)
			}
			return true
		})
	}
	seenReads := map[string]bool{}
	for _, expr := range other {
		for _, ref := range resolveReferences(references(expr), plan, metadata, aliases) {
			if !seenReads[ref] {
				seenReads[ref] = true
				out.AdditionalReads = append(out.AdditionalReads, ref)
			}
		}
	}
	sort.Strings(out.AdditionalReads)
	for _, col := range targetColumns {
		if seenOutput[col.Name] {
			continue
		}
		entry := col.Name + " (" + col.Type + ")"
		if col.DefaultKind != "" {
			entry += " " + col.DefaultKind + " " + col.DefaultExpression
		} else {
			entry += " (not identified in SELECT outputs)"
		}
		out.TargetExtras = append(out.TargetExtras, entry)
	}
	return out
}
