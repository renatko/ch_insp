package clickhouse

import (
	"fmt"
	"strings"

	parser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

type lineageRelation struct {
	table   *lineageTable
	derived *lineageScope
	alias   string
}

type lineageScope struct {
	query      *parser.SelectQuery
	relations  []lineageRelation
	aliases    map[string][]parser.Expr
	scalars    map[*parser.SelectQuery]*lineageScope
	incomplete bool
}

type scopeBuilder struct {
	database string
	physical []lineageTable
	warnings []string
	stack    map[*parser.SelectQuery]bool
}

func (b *scopeBuilder) build(query *parser.SelectQuery, inherited map[string]*parser.SelectQuery) *lineageScope {
	scope := &lineageScope{query: query, aliases: map[string][]parser.Expr{}, scalars: map[*parser.SelectQuery]*lineageScope{}}
	if query == nil {
		scope.incomplete = true
		return scope
	}
	if b.stack[query] {
		scope.incomplete = true
		b.warnings = append(b.warnings, "Recursive CTE requires manual lineage inspection.")
		return scope
	}
	b.stack[query] = true
	defer delete(b.stack, query)
	ctes := make(map[string]*parser.SelectQuery, len(inherited))
	for name, q := range inherited {
		ctes[name] = q
	}
	if query.With != nil {
		for _, item := range query.With.CTEs {
			if id, ok := item.Expr.(*parser.Ident); ok {
				switch q := item.Alias.(type) {
				case *parser.SelectQuery:
					ctes[id.Name] = q
					continue
				case *parser.SubQuery:
					if q.Select != nil {
						ctes[id.Name] = q.Select
						continue
					}
				}
			}
			if id, ok := item.Alias.(*parser.Ident); ok {
				scope.aliases[id.Name] = append(scope.aliases[id.Name], item.Expr)
			} else {
				scope.incomplete = true
				b.warnings = append(b.warnings, "Unsupported WITH expression requires manual inspection.")
			}
		}
	}
	for _, item := range query.SelectItems {
		if item.Alias != nil {
			scope.aliases[item.Alias.Name] = append(scope.aliases[item.Alias.Name], item.Expr)
		}
	}
	if query.UnionAll != nil || query.UnionDistinct != nil || query.Except != nil || query.ArrayJoin != nil {
		scope.incomplete = true
		b.warnings = append(b.warnings, "UNION, EXCEPT or ARRAY JOIN requires manual lineage inspection.")
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
			switch v := value.(type) {
			case *parser.TableIdentifier:
				if v == nil || v.Table == nil {
					scope.incomplete = true
					return
				}
				if cte, ok := ctes[v.Table.Name]; ok && v.Database == nil {
					if alias == "" {
						alias = v.Table.Name
					}
					scope.relations = append(scope.relations, lineageRelation{alias: alias, derived: b.build(cte, ctes)})
				} else {
					table := tableFromIdentifier(v, b.database)
					table.Alias = alias
					b.physical = append(b.physical, table)
					scope.relations = append(scope.relations, lineageRelation{alias: alias, table: &table})
				}
			case *parser.SubQuery:
				if v != nil && v.Select != nil {
					scope.relations = append(scope.relations, lineageRelation{alias: alias, derived: b.build(v.Select, ctes)})
				} else {
					scope.incomplete = true
				}
			case *parser.SelectQuery:
				scope.relations = append(scope.relations, lineageRelation{alias: alias, derived: b.build(v, ctes)})
			default:
				scope.incomplete = true
				b.warnings = append(b.warnings, "A table function or unsupported FROM source requires manual lineage inspection.")
			}
		case nil:
		default:
			scope.incomplete = true
			b.warnings = append(b.warnings, "Unsupported FROM structure requires manual lineage inspection.")
		}
	}
	if query.From != nil {
		collect(query.From.Expr)
	}
	var expressions []parser.Expr
	for _, item := range query.SelectItems {
		expressions = append(expressions, item.Expr)
	}
	for _, expressionsByAlias := range scope.aliases {
		expressions = append(expressions, expressionsByAlias...)
	}
	if query.Where != nil {
		expressions = append(expressions, query.Where)
	}
	if query.Prewhere != nil {
		expressions = append(expressions, query.Prewhere)
	}
	if query.Having != nil {
		expressions = append(expressions, query.Having)
	}
	for _, expr := range expressions {
		for _, subquery := range directSubqueries(expr) {
			if subquery.Select != nil && scope.scalars[subquery.Select] == nil {
				scope.scalars[subquery.Select] = b.build(subquery.Select, ctes)
			}
		}
	}
	return scope
}

type referenceResult struct {
	inputs           []string
	found, uncertain bool
	reason           string
}

// resolveExpression follows aliases and derived SELECT outputs back to tables.
func resolveExpression(expr parser.Expr, scope *lineageScope, metadata map[string][]tableColumn, stack map[string]bool) []string {
	if expr == nil {
		return nil
	}
	if hasUnscopedLambda(expr) {
		return []string{expr.String() + " (unresolved: unsupported lambda scope)"}
	}
	refs := references(expr)
	var result []string
	seen := map[string]bool{}
	for _, ref := range refs {
		resolved := resolveScopedReference(scope, ref, metadata, stack)
		if resolved.uncertain {
			label := ref.name
			if ref.qualifier != "" {
				label = ref.qualifier + "." + ref.name
			}
			reason := resolved.reason
			if reason == "" {
				reason = "unresolved: source cannot be identified; inspect SQL and metadata"
			}
			if len(resolved.inputs) > 0 {
				reason += ": " + strings.Join(resolved.inputs, ", ")
			}
			resolved.inputs = []string{label + " (" + reason + ")"}
		} else if !resolved.found {
			label := ref.name
			if ref.qualifier != "" {
				label = ref.qualifier + "." + ref.name
			}
			resolved.inputs = []string{label + " (unresolved)"}
		}
		for _, value := range resolved.inputs {
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	for _, subquery := range directSubqueries(expr) {
		inner := scope.scalars[subquery.Select]
		if inner == nil || inner.incomplete || inner.query == nil || len(inner.query.SelectItems) != 1 {
			result = append(result, subquery.String()+" (unresolved: scalar subquery)")
			continue
		}
		item := inner.query.SelectItems[0]
		inputs := resolveExpression(item.Expr, inner, metadata, stack)
		for _, ref := range collectScopeReads(inner, metadata) {
			inputs = append(inputs, ref+" [subquery condition]")
		}
		for _, value := range withTransformation(inputs, "scalar subquery", item.Expr.String()) {
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	return result
}

func directSubqueries(expr parser.Expr) []*parser.SubQuery {
	var all []*parser.SubQuery
	seen := map[*parser.SubQuery]bool{}
	parser.Walk(expr, func(n parser.Expr) bool {
		if sub, ok := n.(*parser.SubQuery); ok && sub.Select != nil && !seen[sub] {
			seen[sub] = true
			all = append(all, sub)
		}
		return true
	})
	nested := map[*parser.SubQuery]bool{}
	for _, parent := range all {
		parser.Walk(parent.Select, func(n parser.Expr) bool {
			if child, ok := n.(*parser.SubQuery); ok && child != parent {
				nested[child] = true
			}
			return true
		})
	}
	var direct []*parser.SubQuery
	for _, sub := range all {
		if !nested[sub] {
			direct = append(direct, sub)
		}
	}
	return direct
}

func resolveScopedReference(scope *lineageScope, ref sqlReference, metadata map[string][]tableColumn, stack map[string]bool) referenceResult {
	if scope == nil || ref.name == "*" {
		return referenceResult{}
	}
	if ref.qualifier == "" {
		aliases := scope.aliases[ref.name]
		if len(aliases) > 0 {
			if len(aliases) > 1 {
				return referenceResult{uncertain: true, reason: "ambiguous alias"}
			}
			alias := aliases[0]
			// A self-renaming `id AS id` refers to the physical input.
			id, self := alias.(*parser.Ident)
			self = self && id.Name == ref.name
			if !self {
				key := fmt.Sprintf("%p:%s", scope, ref.name)
				if stack[key] {
					return referenceResult{uncertain: true, reason: "unresolved: cyclic alias"}
				}
				physical := resolveFromRelations(scope, ref, metadata, stack)
				if physical.uncertain {
					return physical
				}
				if physical.found {
					return referenceResult{uncertain: true, inputs: physical.inputs, reason: "ambiguous: alias and source column share this name; check prefer_column_name_to_alias"}
				}
				stack[key] = true
				values := resolveExpression(alias, scope, metadata, stack)
				delete(stack, key)
				return referenceResult{inputs: withTransformation(values, ref.name, alias.String()), found: true}
			}
		}
	}
	return resolveFromRelations(scope, ref, metadata, stack)
}

func resolveFromRelations(scope *lineageScope, ref sqlReference, metadata map[string][]tableColumn, stack map[string]bool) referenceResult {
	var candidates []referenceResult
	uncertain := scope.incomplete
	reason := ""
	var missing []string
	if scope.incomplete {
		reason = "unresolved: unsupported query scope"
	}
	for _, rel := range scope.relations {
		if ref.qualifier != "" {
			matches := ref.qualifier == rel.alias && rel.alias != ""
			if rel.table != nil {
				matches = matches || ref.qualifier == rel.table.Name || ref.qualifier == rel.table.key()
			}
			if !matches {
				continue
			}
		}
		if rel.table != nil {
			cols, ok := metadata[rel.table.key()]
			if !ok {
				uncertain = true
				missing = append(missing, rel.table.key())
				continue
			}
			for _, col := range cols {
				if col.Name == ref.name {
					candidates = append(candidates, referenceResult{found: true, inputs: []string{displaySourceColumn(*rel.table, col, physicalRelations(scope))}})
					break
				}
			}
		} else if rel.derived != nil {
			candidate := resolveDerivedOutput(rel.derived, ref.name, metadata, stack)
			if candidate.uncertain {
				uncertain = true
				reason = candidate.reason
			}
			if candidate.found {
				candidates = append(candidates, candidate)
			}
		} else {
			uncertain = true
			reason = "unresolved: unsupported source"
		}
	}
	if len(missing) > 0 {
		reason = "unresolved: source metadata incomplete for " + strings.Join(missing, ", ") + "; check system.columns access"
	}
	if len(candidates) > 1 {
		var inputs []string
		for _, candidate := range candidates {
			inputs = append(inputs, candidate.inputs...)
		}
		reason := "ambiguous: column occurs in multiple sources"
		if len(missing) > 0 {
			reason += "; metadata also missing for " + strings.Join(missing, ", ")
		}
		return referenceResult{inputs: inputs, uncertain: true, reason: reason}
	}
	if len(candidates) == 1 {
		result := candidates[0]
		result.uncertain = result.uncertain || uncertain
		if uncertain {
			result.reason = reason
		}
		return result
	}
	return referenceResult{uncertain: uncertain, reason: reason}
}

func resolveDerivedOutput(scope *lineageScope, name string, metadata map[string][]tableColumn, stack map[string]bool) referenceResult {
	if scope.query == nil || scope.incomplete {
		return referenceResult{uncertain: true, reason: "unresolved: unsupported derived query"}
	}
	var matches []*parser.SelectItem
	wildcards := 0
	for _, item := range scope.query.SelectItems {
		if item.Expr.String() == "*" || strings.HasSuffix(item.Expr.String(), ".*") {
			wildcards++
		}
		if item.Alias != nil && item.Alias.Name == name {
			matches = append(matches, item)
			continue
		}
		if item.Alias == nil && selectOutputName(item.Expr) == name {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return referenceResult{uncertain: true, reason: "ambiguous: duplicate derived output name"}
	}
	if len(matches) == 1 {
		if wildcards > 0 {
			if reason := wildcardOutputCollision(scope, name, metadata); reason != "" {
				return referenceResult{uncertain: true, reason: reason}
			}
		}
		item := matches[0]
		key := fmt.Sprintf("%p:output:%s", scope, name)
		if stack[key] {
			return referenceResult{uncertain: true, reason: "unresolved: cyclic derived column reference"}
		}
		stack[key] = true
		inputs := resolveExpression(item.Expr, scope, metadata, stack)
		delete(stack, key)
		return referenceResult{found: true, inputs: withTransformation(inputs, name, item.Expr.String())}
	}
	if wildcards > 1 {
		return referenceResult{uncertain: true, reason: "unresolved: multiple wildcard projections require manual inspection"}
	}
	for _, item := range scope.query.SelectItems {
		expression := item.Expr.String()
		if expression == "*" || strings.HasSuffix(expression, ".*") {
			if len(item.Modifiers) > 0 {
				return referenceResult{uncertain: true, reason: "unresolved: wildcard modifiers require manual inspection"}
			}
			qualifier := ""
			if expression != "*" {
				if nested, ok := item.Expr.(*parser.NestedIdentifier); ok && nested.Ident != nil {
					qualifier = nested.Ident.Name
				} else {
					return referenceResult{uncertain: true, reason: "unresolved: unsupported qualified wildcard"}
				}
			}
			var candidates []lineageRelation
			for _, rel := range scope.relations {
				if qualifier == "" || qualifier == rel.alias || (rel.table != nil && (qualifier == rel.table.Name || qualifier == rel.table.key())) {
					candidates = append(candidates, rel)
				}
			}
			if len(candidates) != 1 {
				return referenceResult{uncertain: true, reason: "unresolved: wildcard spans multiple or unknown inputs"}
			}
			if candidates[0].table != nil {
				table := candidates[0].table
				cols, ok := metadata[table.key()]
				if !ok {
					return referenceResult{uncertain: true, reason: "unresolved: source metadata incomplete for " + table.key() + "; check system.columns access"}
				}
				for _, col := range cols {
					if col.Name == name && (col.DefaultKind == "ALIAS" || col.DefaultKind == "MATERIALIZED" || col.DefaultKind == "EPHEMERAL") {
						return referenceResult{uncertain: true, reason: "unresolved: wildcard inclusion of " + col.DefaultKind + " columns depends on ClickHouse settings"}
					}
				}
			}
			ref := sqlReference{name: name, qualifier: qualifier}
			result := resolveFromRelations(scope, ref, metadata, stack)
			if result.found && !result.uncertain {
				result.inputs = withTransformation(result.inputs, name, expression)
			}
			return result
		}
	}
	return referenceResult{}
}

func wildcardOutputCollision(scope *lineageScope, name string, metadata map[string][]tableColumn) string {
	for _, item := range scope.query.SelectItems {
		expression := item.Expr.String()
		if expression != "*" && !strings.HasSuffix(expression, ".*") {
			continue
		}
		if len(item.Modifiers) > 0 {
			return "unresolved: wildcard modifiers require manual inspection"
		}
		qualifier := ""
		if expression != "*" {
			if nested, ok := item.Expr.(*parser.NestedIdentifier); ok && nested.Ident != nil {
				qualifier = nested.Ident.Name
			} else {
				return "unresolved: unsupported qualified wildcard"
			}
		}
		var candidates []lineageRelation
		for _, rel := range scope.relations {
			if qualifier == "" || qualifier == rel.alias || (rel.table != nil && (qualifier == rel.table.Name || qualifier == rel.table.key())) {
				candidates = append(candidates, rel)
			}
		}
		if len(candidates) != 1 || candidates[0].table == nil {
			return "unresolved: cannot check wildcard output against derived or multiple inputs"
		}
		table := candidates[0].table
		cols, ok := metadata[table.key()]
		if !ok {
			return "unresolved: source metadata incomplete for " + table.key() + "; check system.columns access"
		}
		for _, col := range cols {
			if col.Name == name {
				return "ambiguous: explicit output and wildcard may both produce " + name
			}
		}
	}
	return ""
}

func selectOutputName(expr parser.Expr) string {
	switch v := expr.(type) {
	case *parser.Ident:
		return v.Name
	case *parser.Path:
		if len(v.Fields) > 0 {
			return v.Fields[len(v.Fields)-1].Name
		}
	case *parser.NestedIdentifier:
		if v.DotIdent != nil {
			return v.DotIdent.Name
		}
		if v.Ident != nil {
			return v.Ident.Name
		}
	}
	return ""
}

func withTransformation(inputs []string, name, expression string) []string {
	if len(inputs) == 0 {
		return []string{fmt.Sprintf("%s = %s (no explicit source columns)", name, expression)}
	}
	result := make([]string, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, input+" [via "+name+" = "+expression+"]")
	}
	return result
}

func physicalRelations(scope *lineageScope) []lineageTable {
	var result []lineageTable
	for _, rel := range scope.relations {
		if rel.table != nil {
			result = append(result, *rel.table)
		}
	}
	return result
}

// Include columns used by filters, joins and grouping inside derived sources.
func collectScopeReads(root *lineageScope, metadata map[string][]tableColumn) []string {
	seen := map[string]bool{}
	var result []string
	var visit func(*lineageScope)
	visit = func(scope *lineageScope) {
		if scope == nil || scope.query == nil {
			return
		}
		q := scope.query
		var clauses []parser.Expr
		if q.Prewhere != nil {
			clauses = append(clauses, q.Prewhere)
		}
		if q.Where != nil {
			clauses = append(clauses, q.Where)
		}
		if q.GroupBy != nil {
			clauses = append(clauses, q.GroupBy)
		}
		if q.Having != nil {
			clauses = append(clauses, q.Having)
		}
		if q.OrderBy != nil {
			clauses = append(clauses, q.OrderBy)
		}
		if q.Window != nil {
			clauses = append(clauses, q.Window)
		}
		if q.LimitBy != nil {
			clauses = append(clauses, q.LimitBy)
		}
		if q.ArrayJoin != nil {
			clauses = append(clauses, q.ArrayJoin)
		}
		var joinClauses func(parser.Expr)
		joinClauses = func(expr parser.Expr) {
			if join, ok := expr.(*parser.JoinExpr); ok && join != nil {
				if join.Constraints != nil {
					clauses = append(clauses, join.Constraints)
				}
				joinClauses(join.Right)
			}
		}
		if q.From != nil {
			joinClauses(q.From.Expr)
		}
		for _, clause := range clauses {
			for _, ref := range resolveExpression(clause, scope, metadata, map[string]bool{}) {
				if !seen[ref] {
					seen[ref] = true
					result = append(result, ref)
				}
			}
		}
		for _, rel := range scope.relations {
			if rel.derived != nil {
				visit(rel.derived)
			}
		}
	}
	visit(root)
	return result
}
