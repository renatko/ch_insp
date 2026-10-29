package ui

import (
	"fmt"
	"strings"
)

func (m model) columnLines() []string {
	source := "Static SQL analysis + system.columns · c: back to diagram"
	if m.schemaPath != "" {
		source = "Static SQL analysis + dump columns · offline · c: back to diagram"
	}
	lines := []string{accentStyle.Render("COLUMN FLOW"), mutedStyle.Render(source), ""}
	if m.columnsLoading {
		return append(lines, mutedStyle.Render("Loading column metadata…"))
	}
	if m.columnsError != nil {
		return append(lines, errorStyle.Render("Cannot analyze view: "+m.columnsError.Error()), mutedStyle.Render("Press d to inspect the original SQL."))
	}
	for _, warning := range m.columnsData.Warnings {
		lines = append(lines, errorStyle.Render("! "+warning))
	}
	if len(m.columnsData.Warnings) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, accentStyle.Render("QUERY CONDITIONS"), mutedStyle.Render("Outer SELECT · SQL formatted by parser"))
	if len(m.columnsData.Joins) == 0 {
		lines = append(lines, mutedStyle.Render("  JOIN: (none)"))
	} else {
		for _, join := range m.columnsData.Joins {
			lines = append(lines, "  "+join)
		}
	}
	if m.columnsData.Prewhere != "" {
		lines = append(lines, "  "+m.columnsData.Prewhere)
	}
	if m.columnsData.Where == "" {
		lines = append(lines, mutedStyle.Render("  WHERE: (none)"))
	} else {
		lines = append(lines, "  "+m.columnsData.Where)
	}
	if m.columnsData.GroupBy == "" {
		lines = append(lines, mutedStyle.Render("  GROUP BY: (none)"))
	} else {
		lines = append(lines, "  "+m.columnsData.GroupBy)
	}
	if m.columnsData.Having != "" {
		lines = append(lines, "  "+m.columnsData.Having)
	}
	lines = append(lines, "")
	for _, column := range m.columnsData.Columns {
		output := column.Output
		if column.ViewType != "" {
			output += "  [MV: " + column.ViewType + "]"
		} else {
			output += "  [MV type not found]"
		}
		if column.TargetType != "" {
			output += " → target: " + column.Output + " (" + column.TargetType + ")"
		} else {
			output += " → " + column.TargetStatus
		}
		lines = append(lines, accentStyle.Render("OUTPUT  "+output), "  SQL:   "+column.Expression)
		if len(column.Inputs) == 0 {
			lines = append(lines, mutedStyle.Render("  INPUT: no explicit column reference"))
		} else {
			for i, input := range column.Inputs {
				label := "         "
				if i == 0 {
					label = "  INPUT: "
				}
				lines = append(lines, label+styleColumnPath(input))
			}
		}
		lines = append(lines, "")
	}
	if len(m.columnsData.Columns) == 0 {
		lines = append(lines, mutedStyle.Render("No SELECT outputs identified."), "")
	}
	if len(m.columnsData.AdditionalReads) > 0 {
		lines = append(lines, accentStyle.Render("OTHER REFERENCES"), mutedStyle.Render("JOIN / WHERE / GROUP BY / ORDER BY and related clauses"))
		for _, ref := range m.columnsData.AdditionalReads {
			lines = append(lines, "  "+styleColumnPath(ref))
		}
		lines = append(lines, "")
	}
	if len(m.columnsData.TargetExtras) > 0 {
		lines = append(lines, accentStyle.Render("OTHER TARGET COLUMNS"), mutedStyle.Render("Not identified among SELECT outputs; inspect defaults and SQL."))
		for _, extra := range m.columnsData.TargetExtras {
			lines = append(lines, "  "+extra)
		}
		lines = append(lines, "")
	}
	lines = append(lines, mutedStyle.Render("Unresolved and ambiguous references require checking the SQL."), mutedStyle.Render(fmt.Sprintf("%d SELECT outputs · d: original SQL", len(m.columnsData.Columns))))
	return lines
}

// Keep multiline ClickHouse errors on separate lines in the detail viewport.
func splitColumnLines(lines []string) []string {
	var out []string
	for _, line := range lines {
		out = append(out, strings.Split(line, "\n")...)
	}
	return out
}

// Wildcard steps carry no transformation and are visually secondary.
func styleColumnPath(path string) string {
	parts := strings.Split(path, " → ")
	if len(parts) < 2 {
		return path
	}
	var out strings.Builder
	out.WriteString(parts[0])
	for _, step := range parts[1:] {
		text := " → " + step
		if strings.HasPrefix(step, "SELECT ") && (step == "SELECT *" || strings.HasSuffix(step, ".*")) {
			out.WriteString(mutedStyle.Render(text))
		} else {
			out.WriteString(text)
		}
	}
	return out.String()
}
