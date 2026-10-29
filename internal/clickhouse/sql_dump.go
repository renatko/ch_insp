package clickhouse

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	parser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// SQLDump contains only static metadata read from a local SQL file. No SQL is executed.
type SQLDump struct {
	Catalog  Catalog
	Warnings []string
	columns  map[string][]tableColumn
}

func LoadSQLDump(path, initialDatabase string) (*SQLDump, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read SQL dump: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32<<20+1))
	if err != nil {
		return nil, fmt.Errorf("read SQL dump: %w", err)
	}
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("SQL dump exceeds 32 MiB")
	}
	statements, err := splitDumpStatements(string(data))
	if err != nil {
		return nil, err
	}
	database := initialDatabase
	if database == "" {
		database = "default"
	}
	out := &SQLDump{columns: map[string][]tableColumn{}}
	databases := map[string]bool{}
	seenTables, seenViews := map[string]bool{}, map[string]bool{}
	for i, statement := range statements {
		stmts, parseErr := parseDumpStatement(statement)
		if parseErr != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("Statement %d: %v", i+1, parseErr))
			continue
		}
		if len(stmts) == 0 {
			continue
		}
		switch v := stmts[0].(type) {
		case *parser.UseStmt:
			database = v.Database.Name
			databases[database] = true
		case *parser.CreateDatabase:
			if name, ok := v.Name.(*parser.Ident); ok {
				databases[name.Name] = true
			} else {
				out.Warnings = append(out.Warnings, fmt.Sprintf("Statement %d: unsupported database name", i+1))
			}
		case *parser.CreateTable:
			if v.Name == nil || v.Name.Table == nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("Statement %d: table name missing", i+1))
				continue
			}
			table := tableFromIdentifier(v.Name, database)
			databases[table.Database] = true
			if seenTables[table.key()] {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s: duplicate table definition skipped", table.key()))
				continue
			}
			seenTables[table.key()] = true
			if v.TableSchema == nil || len(v.TableSchema.Columns) == 0 {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s: columns unavailable (AS/table function or incomplete definition)", table.key()))
				continue
			}
			var cols []tableColumn
			for _, expr := range v.TableSchema.Columns {
				c, ok := expr.(*parser.ColumnDef)
				if !ok || c.Name == nil || c.Type == nil {
					continue
				}
				name := c.Name.String()
				if c.Name.Ident != nil && c.Name.DotIdent == nil {
					name = c.Name.Ident.Name
				}
				col := tableColumn{Name: name, Type: c.Type.String()}
				switch {
				case c.DefaultExpr != nil:
					col.DefaultKind = "DEFAULT"
					col.DefaultExpression = c.DefaultExpr.String()
				case c.MaterializedExpr != nil:
					col.DefaultKind = "MATERIALIZED"
					col.DefaultExpression = c.MaterializedExpr.String()
				case c.AliasExpr != nil:
					col.DefaultKind = "ALIAS"
					col.DefaultExpression = c.AliasExpr.String()
				}
				cols = append(cols, col)
			}
			out.columns[table.key()] = cols
		case *parser.CreateMaterializedView:
			if v.Name == nil || v.Name.Table == nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("Statement %d: view name missing", i+1))
				continue
			}
			table := tableFromIdentifier(v.Name, database)
			databases[table.Database] = true
			if seenViews[table.key()] {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s: duplicate MV definition skipped", table.key()))
				continue
			}
			seenViews[table.key()] = true
			out.Catalog.Views = append(out.Catalog.Views, View{Database: table.Database, Name: table.Name, Engine: "MaterializedView", Definition: statement})
		default:
			out.Warnings = append(out.Warnings, fmt.Sprintf("Statement %d: unsupported %T; skipped", i+1, stmts[0]))
		}
	}
	for name := range databases {
		out.Catalog.Databases = append(out.Catalog.Databases, name)
	}
	sort.Strings(out.Catalog.Databases)
	sort.Slice(out.Catalog.Views, func(i, j int) bool {
		a, b := out.Catalog.Views[i], out.Catalog.Views[j]
		if a.Database != b.Database {
			return a.Database < b.Database
		}
		return a.Name < b.Name
	})
	out.Catalog.CurrentDatabase = initialDatabase
	if out.Catalog.CurrentDatabase == "" {
		if len(out.Catalog.Views) > 0 {
			out.Catalog.CurrentDatabase = out.Catalog.Views[0].Database
		} else if len(out.Catalog.Databases) > 0 {
			out.Catalog.CurrentDatabase = out.Catalog.Databases[0]
		}
	}
	if len(out.Catalog.Databases) == 0 {
		return out, fmt.Errorf("SQL dump contains no supported schema objects (%d statements skipped); inspect dump syntax", len(out.Warnings))
	}
	return out, nil
}

func parseDumpStatement(sql string) (stmts []parser.Expr, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("SQL parser could not process statement")
		}
	}()
	return parser.NewParser(sql).ParseStmts()
}

// Split on semicolons outside quoted strings, identifiers and SQL comments.
func splitDumpStatements(input string) ([]string, error) {
	var result []string
	start := 0
	var quote byte
	line, block := false, false
	for i := 0; i < len(input); i++ {
		c := input[i]
		if line {
			if c == '\n' {
				line = false
			}
			continue
		}
		if block {
			if c == '*' && i+1 < len(input) && input[i+1] == '/' {
				block = false
				i++
			}
			continue
		}
		if quote != 0 {
			if c == '\\' && i+1 < len(input) {
				i++
				continue
			}
			if c == quote {
				if i+1 < len(input) && input[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if (c == '-' && i+1 < len(input) && input[i+1] == '-') || c == '#' {
			line = true
			if c == '-' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(input) && input[i+1] == '*' {
			block = true
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == ';' {
			if s := strings.TrimSpace(input[start:i]); s != "" {
				result = append(result, s)
			}
			start = i + 1
		}
	}
	if quote != 0 || block {
		return nil, fmt.Errorf("SQL dump contains an unterminated quote or comment")
	}
	if s := strings.TrimSpace(input[start:]); s != "" {
		result = append(result, s)
	}
	return result, nil
}

func (d *SQLDump) ColumnLineage(view View) (ColumnLineage, error) {
	plan, err := parseLineage(view)
	if err != nil {
		return ColumnLineage{}, err
	}
	return analyzeColumns(view, plan, d.columns, []string{"Offline SQL dump: metadata and settings may be incomplete; server EXPLAIN unavailable."}), nil
}
func (d *SQLDump) TargetColumns(database, table string, views []View) (TargetColumns, error) {
	target := TargetColumns{Database: database, Table: table}
	cols, ok := d.columns[database+"."+table]
	if !ok || len(cols) == 0 {
		return target, fmt.Errorf("columns for %s.%s are unavailable in the SQL dump", database, table)
	}
	for _, c := range cols {
		target.Columns = append(target.Columns, TargetColumn{Name: c.Name, Type: c.Type, DefaultKind: c.DefaultKind, DefaultExpression: c.DefaultExpression})
	}
	for _, view := range views {
		if InferFlow(view).Target != database+"."+table {
			continue
		}
		name := view.Database + "." + view.Name
		target.Views = append(target.Views, name)
		lineage, err := d.ColumnLineage(view)
		if err != nil {
			target.Warnings = append(target.Warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		addTargetWriters(&target, name, lineage)
	}
	return target, nil
}
