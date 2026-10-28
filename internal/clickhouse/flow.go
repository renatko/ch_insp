package clickhouse

import (
	"strings"
	"unicode"
)

// Flow describes relationships inferred from a CREATE MATERIALIZED VIEW query.
type Flow struct {
	Sources []string
	Target  string
	Trigger string // inferred only for supported incremental views
}

// InferFlow recognizes simple table references. Complex SQL remains visible in
// the definition; the diagram is deliberately labelled as inferred.
func InferFlow(view View) Flow {
	tokens := sqlTokens(view.Definition)
	flow := Flow{}
	seen := map[string]bool{}
	ctes := map[string]bool{}
	for i := 0; i+3 < len(tokens); i++ {
		if identifier(tokens[i]) && strings.EqualFold(tokens[i+1], "AS") && tokens[i+2] == "(" && (strings.EqualFold(tokens[i+3], "SELECT") || strings.EqualFold(tokens[i+3], "WITH")) {
			ctes[strings.ToLower(tokens[i])] = true
		}
	}
	hasTO := false
	selectAt := -1
	for i, token := range tokens {
		if strings.EqualFold(token, "SELECT") {
			selectAt = i
			break
		}
		if strings.EqualFold(token, "TO") && i+1 < len(tokens) {
			hasTO = true
			if name, _ := tableName(tokens, i+1, view.Database); name != "" {
				flow.Target = name
			}
		}
	}
	if flow.Target == "" {
		if hasTO {
			flow.Target = "unknown target"
		} else {
			flow.Target = "internal storage (no TO)"
		}
	}
	if selectAt < 0 {
		return flow
	}
	for i := selectAt; i+1 < len(tokens); i++ {
		if !strings.EqualFold(tokens[i], "FROM") && !strings.EqualFold(tokens[i], "JOIN") {
			continue
		}
		name, next := tableName(tokens, i+1, view.Database)
		if ctes[strings.ToLower(tokens[i+1])] && next == i+2 {
			continue
		}
		if name == "" || (next < len(tokens) && tokens[next] == "(") {
			continue
		}
		if !seen[name] {
			seen[name] = true
			flow.Sources = append(flow.Sources, name)
		}
	}
	// The token scanner finds sources in unsupported SQL; the scope builder
	// adds physical inputs from parsed structures such as comma joins.
	if plan, err := parseLineage(view); err == nil {
		for _, table := range plan.sources {
			if !seen[table.key()] {
				seen[table.key()] = true
				flow.Sources = append(flow.Sources, table.key())
			}
		}
		flow.Trigger = inferTrigger(tokens, plan)
	}
	return flow
}

// Follow the left-most input through supported CTEs and FROM subqueries.
// Refreshable and unsupported queries retain an unknown trigger.
func inferTrigger(tokens []string, plan lineagePlan) string {
	for _, token := range tokens {
		if strings.EqualFold(token, "SELECT") {
			break
		}
		if strings.EqualFold(token, "REFRESH") {
			return ""
		}
	}
	scope := plan.scope
	for scope != nil && !scope.incomplete && len(scope.relations) > 0 {
		first := scope.relations[0]
		if first.table != nil {
			return first.table.key()
		}
		scope = first.derived
	}
	return ""
}

func tableName(tokens []string, i int, database string) (string, int) {
	if i >= len(tokens) || !identifier(tokens[i]) {
		return "", i
	}
	first := tokens[i]
	i++
	if i+1 < len(tokens) && tokens[i] == "." && identifier(tokens[i+1]) {
		return first + "." + tokens[i+1], i + 2
	}
	return database + "." + first, i
}

func identifier(token string) bool {
	return token != "" && token != "(" && token != ")" && token != "," && token != "." && token != ";" && token != "<string>"
}

func sqlTokens(sql string) []string {
	runes := []rune(sql)
	var tokens []string
	for i := 0; i < len(runes); {
		c := runes[i]
		if unicode.IsSpace(c) {
			i++
			continue
		}
		if i+1 < len(runes) && c == '-' && runes[i+1] == '-' {
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(runes) && c == '/' && runes[i+1] == '*' {
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			if i+1 < len(runes) {
				i += 2
			}
			continue
		}
		if c == '\'' || c == '`' || c == '"' {
			quote := c
			i++
			var b strings.Builder
			for i < len(runes) {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
					b.WriteRune(runes[i])
					i++
					continue
				}
				if runes[i] == quote {
					i++
					if i < len(runes) && runes[i] == quote {
						b.WriteRune(quote)
						i++
						continue
					}
					break
				}
				b.WriteRune(runes[i])
				i++
			}
			if quote == '\'' {
				tokens = append(tokens, "<string>")
			} else {
				tokens = append(tokens, b.String())
			}
			continue
		}
		if strings.ContainsRune("().,;", c) {
			tokens = append(tokens, string(c))
			i++
			continue
		}
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '$' {
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_' || runes[i] == '$') {
				i++
			}
			tokens = append(tokens, string(runes[start:i]))
			continue
		}
		i++
	}
	return tokens
}
