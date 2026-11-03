# Usage and navigation

Commands using `go run` and paths such as `demo/schema.sql` assume the working
directory is the root of a source checkout.

## Connecting and offline mode

Go 1.27.3 or newer is required when building from source or using `go run`.
Prebuilt releases do not require Go. Build the executable with `go build .`, or run the project directly with `go run .`.

To inspect an existing SQL schema dump without connecting to ClickHouse, pass a local SQL file:

```sh
./ch_insp --schema ./schema.sql
```

For an example, open the checked-in standalone demo schema with `go run . --schema ./demo/schema.sql`. It contains DDL only and does not require Docker or a running ClickHouse server.

The file is read and parsed locally; its statements are never executed. `CREATE DATABASE`, `USE`, `CREATE TABLE` and `CREATE MATERIALIZED VIEW` provide the catalog, table columns and view definitions. Unqualified names use the most recent `USE`, or `default` when no `USE` is present; pass `--database NAME` to choose the initial context for unqualified names. The initial database displayed is selected from the imported catalog. The file may be up to 32 MiB. Unsupported statements and parse errors are reported as import warnings; the diagram and column lineage are best-effort and incomplete if the dump omits referenced objects or uses unsupported SQL. The original definition remains available via `d`.

The header says `OFFLINE SQL`: execution status, rates, historical charts, live node selection and server-side `EXPLAIN` are unavailable. `r` rereads the file. `--schema` cannot be combined with connection flags; an existing `CLICKHOUSE_URL` is ignored in this mode. This feature reads an existing SQL file and does not create or export a dump.

Set a ClickHouse DSN in `CLICKHOUSE_URL`, then start the TUI:

```sh
export CLICKHOUSE_URL='clickhouse://user:password@localhost:9000/default'
go run .
```

Alternatively, pass connection options on the command line:

```sh
go run . --host localhost --port 9000 --user default --database default
```

Available options are `--url`, `--host`, `--port`, `--user`, `--password`,
`--database` and `--secure` (TLS). Run `go run . --help` for usage.
`--url` overrides `CLICKHOUSE_URL`, and individual options override the
corresponding URL fields. With individual options and no URL, defaults are
`localhost:9000`, user `default` and database `default`; without any connection
settings the application asks for a URL. A password passed with `--password`
or `--url` can be visible in process arguments and shell history. For a
password, prefer setting `CLICKHOUSE_URL` in the environment.

The application reads `system.databases` and `system.tables` to list databases and materialized views and show each
view's inferred source-to-view-to-target diagram and `CREATE` definition. It only runs read-only queries. The
configured ClickHouse user should have permission to read the relevant system
metadata.

## Navigation and SQL

The overview stays in the background while details and selectors open in a centered panel. Filters, search and selection use compact panels; diagrams, SQL, column flow, targets, dependencies and errors use a wider panel with seven columns of margin on each side. The frame shows the navigation path; detail titles stay visible while scrolling. Press `Esc` to return to the previous view; on small terminals screens use the available space directly. Mouse clicks and scrolling apply to the open panel.

The initial database is the connection's current database. Press `b` to choose
another database, including one with no materialized views. The active database
is shown in the header; names in the list and diagram are unqualified. A view
can reference another database, so identical short names can represent different
tables. The SQL definition remains unchanged and shows the full references.

The diagram recognizes simple table references after `FROM` and `JOIN` and a
target after `TO` in the stored SQL. Complex expressions, table functions,
aliases and other SQL constructs may not be resolved completely. Sources are
query inputs, not necessarily insert triggers: for incremental materialized
views a joined table does not trigger the view when it receives inserts. The diagram marks the leftmost physical input of a supported incremental MV, including simple CTEs and subqueries as `[trigger]` and other query inputs as `[input]`; unsupported or refreshable definitions show `trigger unknown`. These roles are inferred from SQL. A
view without `TO` uses internal storage. Check the SQL definition with `d`. The SQL viewer adds line breaks to parser-supported definitions; press `v` to switch between formatted and original text. Unsupported definitions are shown as originally received. Formatting never changes or executes the definition.

## Overview, search and filters

The initial screen is a flow overview for every MV in the selected database; press `o` to switch between it and the selected MV. The compact overview uses `[T]` for an inferred trigger, `[I]` for another input, `R`/`W` for MV read/write rates, and `ok`/`err` for observed MV status in the selected error window. Rates are rows/s; charts show rows per completed interval. Full timestamps and diagnostics remain available in the MV detail and error screens. Views
that write to the same target are grouped and connect to one target. Each MV
uses a line for each input, with its rate directly below it; the MV and its
rates appear alongside the first input. Multiple inputs connect to the same
MV. When the trigger is known, other inputs and their rates are indented; when
it is unknown, all inputs remain at the same level. The
target's total rate is shown directly below the target. Inputs may be repeated
for different views. Sources are bold, MV names italic, and targets regular;
the terminal and font determine whether these styles are visible. Each target
group has its own color, including its inputs and
views. Colors remain stable when filtering; with more than eight groups the
palette repeats, and refreshing after schema changes can reassign colors.
Select an MV with `↑` / `↓` and press `Enter` to open its diagram. Press `→` to select its target, then `Enter` to open the target table; `↑` / `↓` move between target groups and `←` returns focus to the MV. Moving the mouse over a flow or target selects it; click the selected item to open it. The selected item is highlighted. Use `H` / `L`, Shift+arrow or Shift+mouse wheel for horizontal scrolling; page scrolling is also supported. The overview help stays at the top and shortcuts stay at the bottom while the diagram scrolls. Metrics are queried in
batches and have the same local-node and log-delay limits described above.

Press `g` to inspect the selected source table’s inferred incoming and outgoing dependencies, or the selected target when target focus is active. For a flow with multiple inputs, choose the desired table from the list. Navigate to connected tables or MVs with Enter, press `t` for the current table’s column view, and go back through tables with Backspace. Dependencies use the visible catalog across schemas and can be incomplete. Opening an MV outside the active filter switches to its database and clears the filter.

From table dependencies, press `i` for downstream table impact across multiple
MV steps. To inspect a column, press `t` in dependencies, select the column and
press `i` (or select a column from a target table). Impact uses all accessible
views, independently of overview filters. Select an MV row and press Enter to
open it; `/` or `f` filters the paths, and
Esc returns to the previous screen. While filtering, Enter or Esc exits editing
and Ctrl+X clears the text. Paths are inferred from SQL, not
observed writes. A source is marked as an inferred trigger, another query input,
or an input whose trigger is unknown. Cycles and analysis limits are marked.

Column impact follows SELECT outputs into target columns matched by name. It
also shows references in JOIN, WHERE, PREWHERE, GROUP BY, HAVING and other
conditions; these can affect the whole result, so downstream table paths are
marked as potential rather than mapped to individual columns. Ambiguous or
unsupported SQL, missing permissions or metadata, inaccessible views and
incomplete SQL dumps can hide relationships. Inspect the warnings and original
definitions; no result does not establish that a change is safe. Analysis is
read-only and also works with `--schema`; it stops after 64 analyzed views,
500 result rows or a 45-second timeout and reports when results are incomplete.

Press `/` to search visible sources, MVs and targets. Use ↑/↓ and Enter to jump to a match, or Esc to cancel; the search respects active filters. Press `Ctrl+X` in search to clear active filters while keeping the search query.

Press `f` to filter views by source and/or target tables. The lists are built
from all discovered views in the current database. Use Tab or click the Source and Target tabs to switch lists;
arrows or the mouse wheel to navigate, and Space or a click to toggle tables. Press `/` to search the current table list, type a name, then press Enter or Esc to leave search editing. The selected table names and the number of MVs matching the draft selection are shown before applying it.
Multiple choices within one list match any selected table; when both lists have
choices, a view must match both. Enter applies the selection; Ctrl+X clears both,
and Esc cancels. `(any table)` selects all entries in the current list; select
it again to clear that list. An empty selection applies no restriction for that
list. Selecting all entries matches only those inferred table names, so views
without a recognized table in that list may be excluded. Identical table names from
different databases include their database in the list. The filter applies to
the list and overview and remains active when switching databases. Filtering
uses the SQL-inferred relationships described above; complex SQL or internal
targets may therefore not be matched.

When filters are active, the overview title shows the selected source and target
tables (or `any` for an unrestricted side). Names include their database; if
space is limited, names are shortened and `+N` indicates additional selections.
In narrow terminals the labels are abbreviated to `S` and `T`. Press `f` to
inspect or change the complete selection.

## Keyboard and mouse

- `↑` / `↓` or `j` / `k`: select a view
- `b`: choose a database; use `↑` / `↓` and `Enter` to switch, `Esc` to cancel
- `/`: search visible sources, MVs and targets
- `f`: edit source and target table filters
- `e`: inspect recent MV errors; `w`: change their observation window
- `g`: inspect inferred table dependencies
- `i`: analyze table impact from dependencies, or selected column impact from a table's column view
- `t`: inspect the selected MV target table
- `n`: select a discovered native TCP node
- `p`: change chart interval; `s`: switch overview chart scale
- `o`: switch between the overview and selected MV; `Enter` opens the selected MV
- `r`: reload view metadata
- `d`: switch between the diagram and SQL definition
- `v`: switch formatted and original SQL while viewing a definition
- `c`: switch between the diagram and column flow in the detailed view
- `Page Up` / `Page Down`: scroll the current detail panel
- `←` / `→` or `h` / `l`: select MV or target in overview; scroll detail horizontally
- `H` / `L` or Shift + `←` / `→`: scroll overview horizontally
- `q` or `Ctrl+C`: quit

Mouse support depends on the terminal. Click an MV in the list to select it,
or hover over a flow or target in the overview and click to open it.
Click a database in the database selector to switch. The bracketed actions in
the footer are clickable. The wheel scrolls the area under the pointer: it
moves the selection over the MV list and scrolls the diagram or SQL elsewhere.
Use horizontal wheel scrolling, or Shift + wheel when supported, to scroll
horizontally. Hold Shift while selecting text if your terminal supports that
override for mouse capture.
