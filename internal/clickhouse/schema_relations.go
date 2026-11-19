package clickhouse

import (
	"regexp"
	"strings"
)

// SchemaRelation is configured metadata, not evidence that data moved. Names
// are kept separate from definitions, which can contain credentials.
type SchemaRelation struct {
	Database, Name, Kind, Other, Detail string
}

var distributedArgs = regexp.MustCompile(`(?i)^\s*Distributed\s*\(\s*('(?:[^'\\]|\\.)*'|[A-Za-z_][\w]*)\s*,\s*('(?:[^'\\]|\\.)*'|[A-Za-z_][\w]*)\s*,\s*('(?:[^'\\]|\\.)*'|[A-Za-z_][\w]*)\s*(?:,|\))`)
var dictionarySource = regexp.MustCompile(`(?i)\bSOURCE\s*\(\s*([A-Za-z_][A-Za-z_0-9]*)\s*\(`)
var dictionaryToken = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|[A-Za-z_][A-Za-z_0-9]*|[()]`)

func simpleLiteral(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
	}
	// Do not guess at escapes or macros.
	if strings.ContainsAny(s, "\\{}'") {
		return ""
	}
	return s
}

// ParseSchemaRelation only resolves simple literal metadata. Complex engine
// expressions and remote dictionary hosts remain explicitly unresolved.
func ParseSchemaRelation(database, name, engine, engineFull, definition string) SchemaRelation {
	r := SchemaRelation{Database: database, Name: name, Kind: engine}
	switch engine {
	case "Distributed":
		r.Detail = "unresolved routing"
		m := distributedArgs.FindStringSubmatch(engineFull)
		if len(m) == 4 {
			cluster, db, table := simpleLiteral(m[1]), simpleLiteral(m[2]), simpleLiteral(m[3])
			if cluster != "" && db != "" && table != "" {
				r.Other = db + "." + table
				r.Detail = "cluster " + cluster + "; remote table (not verified)"
			}
		}
	case "Dictionary":
		r.Detail = "unknown source"
		src := dictionarySource.FindStringSubmatch(definition)
		if len(src) != 2 {
			return r
		}
		kind := strings.ToUpper(src[1])
		r.Detail = kind + " source (external or unresolved)"
		if kind != "CLICKHOUSE" {
			return r
		}
		fields := map[string]string{}
		loc := dictionarySource.FindStringSubmatchIndex(definition)
		tokens := dictionaryToken.FindAllString(definition[loc[1]:], -1)
		depth := 1
		for i := 0; i < len(tokens); i++ {
			switch tokens[i] {
			case "(":
				depth++
			case ")":
				depth--
			default:
				key := strings.ToUpper(tokens[i])
				if depth == 1 && (key == "HOST" || key == "DB" || key == "TABLE") && i+1 < len(tokens) && strings.HasPrefix(tokens[i+1], "'") {
					fields[key] = simpleLiteral(tokens[i+1])
					i++
				} else if i+1 < len(tokens) && strings.HasPrefix(tokens[i+1], "'") {
					i++
				}
			}
			if depth == 0 {
				break
			}
		}
		host := strings.ToLower(fields["HOST"])
		if (host == "localhost" || host == "127.0.0.1" || host == "::1") && fields["DB"] != "" && fields["TABLE"] != "" {
			r.Other = fields["DB"] + "." + fields["TABLE"]
			r.Detail = "ClickHouse source on localhost (configured)"
		}
	}
	return r
}

var dictionaryCreate = regexp.MustCompile(`(?is)^\s*CREATE\s+DICTIONARY\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)?)`)
var distributedEngine = regexp.MustCompile(`(?is)\bENGINE\s*=\s*(Distributed\s*\([^)]*\))`)

func dictionaryName(name, fallback string) (string, string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return fallback, name
}
