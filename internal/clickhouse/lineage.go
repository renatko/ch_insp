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
	Joins           []string
	Prewhere        string
	Where           string
	GroupBy         string
	Having          string
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
	scope    *lineageScope
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
	builder := scopeBuilder{database: view.Database, stack: map[*parser.SelectQuery]bool{}}
	plan.scope = builder.build(plan.query, nil)
	plan.sources = builder.physical
	plan.complex = plan.scope.incomplete
	plan.warnings = append(plan.warnings, builder.warnings...)

	return plan, nil
}

func tableFromIdentifier(id *parser.TableIdentifier, fallback string) lineageTable {
	database := fallback
	if id.Database != nil {
		database = id.Database.Name
	}
	return lineageTable{Database: database, Name: id.Table.Name}
}

// LoadColumnLineage reads metadata and may analyze the SELECT using EXPLAIN.
// It never executes the view SELECT.
func LoadColumnLineage(ctx context.Context, dsn string, view View) (ColumnLineage, error) {
	result, err := loadColumnLineage(ctx, dsn, view)
	for i, warning := range result.Warnings {
		result.Warnings[i] = RedactConnectionError(fmt.Errorf("%s", warning), dsn).Error()
	}
	return result, RedactConnectionError(err, dsn)
}
func loadColumnLineage(ctx context.Context, dsn string, view View) (ColumnLineage, error) {
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
	var setting string
	var preference *aliasPreference
	if err := db.QueryRowContext(ctx, "SELECT value FROM system.settings WHERE name = 'prefer_column_name_to_alias'").Scan(&setting); err == nil && (setting == "0" || setting == "1") {
		preference = &aliasPreference{preferColumn: setting == "1"}
		if !setAliasPreference(plan.scope, preference) {
			clearAliasPreference(plan.scope)
			preference = nil
		}
	}
	result := analyzeColumns(view, plan, metadata, warnings)
	if preference != nil && preference.used {
		result.Warnings = append(result.Warnings, "Alias collisions resolved using current connection setting prefer_column_name_to_alias="+setting+"; MV execution settings may differ.")
	}
	enrichWithExplain(ctx, db, view, plan, metadata, &result)
	return result, nil
}

type sqlReference struct{ qualifier, name string }

// references records identifier nodes while excluding function names, aliases,
// type names and the components of qualified paths.
func references(expr parser.Expr) []sqlReference {
	ignored := map[*parser.Ident]bool{}
	ignoredPaths := map[*parser.Path]bool{}
	ignoredNested := map[*parser.NestedIdentifier]bool{}
	var identifiers []*parser.Ident
	var refs []sqlReference
	parser.Walk(expr, func(n parser.Expr) bool {
		switch v := n.(type) {
		case *parser.SubQuery:
			parser.Walk(v.Select, func(inner parser.Expr) bool {
				switch id := inner.(type) {
				case *parser.Ident:
					ignored[id] = true
				case *parser.Path:
					ignoredPaths[id] = true
				case *parser.NestedIdentifier:
					ignoredNested[id] = true
				}
				return true
			})
		case *parser.FunctionExpr:
			ignored[v.Name] = true
		case *parser.ColumnExpr:
			if v.Alias != nil {
				ignored[v.Alias] = true
			}
			// The parser represents a lambda as a binary expression nested in
			// one function argument. Its bound names are local to that argument.
			bound := lambdaBindings(v.Expr)
			if len(bound) > 0 {
				parser.Walk(v.Expr, func(x parser.Expr) bool {
					switch param := x.(type) {
					case *parser.Ident:
						if bound[param.Name] {
							ignored[param] = true
						}
					case *parser.Path:
						if len(param.Fields) > 0 && bound[param.Fields[0].Name] {
							ignoredPaths[param] = true
						}
					case *parser.NestedIdentifier:
						if param.Ident != nil && bound[param.Ident.Name] {
							ignoredNested[param] = true
						}
					}
					return true
				})
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
			if len(v.Fields) > 0 && !ignoredPaths[v] {
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
				if !ignoredNested[v] {
					refs = append(refs, sqlReference{qualifier: v.Ident.Name, name: v.DotIdent.Name})
				}
			} else {
				if !ignoredNested[v] {
					refs = append(refs, sqlReference{name: v.Ident.Name})
				}
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

// Follow only binary operators here: descending into nested function calls
// would extend the scope of a lambda to unrelated outer arguments.
func lambdaBindings(expr parser.Expr) map[string]bool {
	bound := map[string]bool{}
	for _, op := range lambdaOperations(expr) {
		parser.Walk(op.LeftExpr, func(param parser.Expr) bool {
			if id, ok := param.(*parser.Ident); ok {
				bound[id.Name] = true
			}
			return true
		})
	}
	return bound
}

func lambdaOperations(expr parser.Expr) []*parser.BinaryOperation {
	var operations []*parser.BinaryOperation
	var visit func(parser.Expr)
	visit = func(node parser.Expr) {
		op, ok := node.(*parser.BinaryOperation)
		if !ok || op == nil {
			return
		}
		if op.Operation == parser.TokenKindArrow {
			operations = append(operations, op)
			return
		}
		visit(op.LeftExpr)
		visit(op.RightExpr)
	}
	visit(expr)
	return operations
}

func hasUnscopedLambda(expr parser.Expr) bool {
	covered := map[*parser.BinaryOperation]bool{}
	ignored := map[*parser.BinaryOperation]bool{}
	parser.Walk(expr, func(n parser.Expr) bool {
		switch v := n.(type) {
		case *parser.ColumnExpr:
			for _, op := range lambdaOperations(v.Expr) {
				covered[op] = true
			}
		case *parser.SubQuery:
			parser.Walk(v.Select, func(inner parser.Expr) bool {
				if op, ok := inner.(*parser.BinaryOperation); ok && op.Operation == parser.TokenKindArrow {
					ignored[op] = true
				}
				return true
			})
		}
		return true
	})
	unscoped := false
	parser.Walk(expr, func(n parser.Expr) bool {
		if op, ok := n.(*parser.BinaryOperation); ok && op.Operation == parser.TokenKindArrow && !covered[op] && !ignored[op] {
			unscoped = true
		}
		return true
	})
	return unscoped
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
	out.Joins = topLevelJoins(plan.query)
	if plan.query.Prewhere != nil {
		out.Prewhere = plan.query.Prewhere.String()
	}
	if plan.query.Where != nil {
		out.Where = plan.query.Where.String()
	}
	if plan.query.GroupBy != nil {
		out.GroupBy = plan.query.GroupBy.String()
	}
	if plan.query.Having != nil {
		out.Having = plan.query.Having.String()
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
			if !plan.complex && len(item.Modifiers) == 0 && plan.scope != nil {
				for _, relation := range plan.scope.relations {
					qualifier := strings.TrimSuffix(expression, ".*")
					if expression == "*" || qualifier == relation.alias || (relation.table != nil && (qualifier == relation.table.Name || qualifier == relation.table.key())) {
						if relation.table == nil {
							candidates = nil
							break
						}
						candidates = append(candidates, *relation.table)
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
		add(output, expression, resolveExpression(item.Expr, plan.scope, metadata, map[string]bool{}), nameKnown)
	}
	out.AdditionalReads = collectScopeReads(plan.scope, metadata)
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

// topLevelJoins only describes the outer SELECT. Nested queries and UNION
// branches remain subject to the warnings in parseLineage.
func topLevelJoins(query *parser.SelectQuery) []string {
	var joins []string
	var visit func(parser.Expr, bool)
	visit = func(expr parser.Expr, first bool) {
		switch node := expr.(type) {
		case *parser.JoinExpr:
			if node == nil {
				return
			}
			if !first && node.Left != nil {
				kind := strings.Join(node.Modifiers, " ")
				if kind == "" {
					kind = "COMMA JOIN"
				}
				entry := kind + " " + node.Left.String()
				if node.Constraints != nil {
					entry += " " + node.Constraints.String()
				}
				joins = append(joins, entry)
			}
			if node.Right != nil {
				visit(node.Right, false)
			}
		case *parser.JoinTableExpr:
			if !first && node != nil {
				joins = append(joins, "COMMA JOIN "+node.String())
			}
		}
	}
	if query.From != nil {
		visit(query.From.Expr, true)
	}
	if query.ArrayJoin != nil {
		joins = append(joins, strings.TrimSpace(query.ArrayJoin.String()))
	}
	return joins
}
