# ch_insp

A read-only terminal UI for exploring ClickHouse materialized views and the data flows between their source and target tables.

![ch_insp demo: explore flows, filter tables, inspect columns and SQL](docs/assets/demo.gif)

## What it does

- Visualize source tables → materialized views → target tables, including shared targets and multiple inputs.
- Search and filter flows; inspect table dependencies, target columns, column lineage and SQL definitions.
- Observe local-node read/write rates, recent history and materialized-view errors when ClickHouse logs are available.
- Switch between discovered cluster nodes, or inspect a SQL schema dump offline without a server.

## Install

Download an archive for Linux, macOS or Windows (amd64 or arm64) from [GitHub Releases](https://github.com/renatko/ch_insp/releases/latest). Extract it and run `ch_insp` (`ch_insp.exe` on Windows). The release also provides SHA-256 checksums; archives include license notices. Binaries are not signed or notarized.

Linux releases also provide `.deb` and `.rpm` packages for amd64 and arm64.
Download the package for your architecture from the release and install it:

```sh
sudo apt install ./ch_insp_VERSION_amd64.deb   # Debian / Ubuntu
sudo dnf install ./ch_insp_VERSION_amd64.rpm   # Fedora / RHEL
```

Replace `VERSION` and `amd64` with the downloaded version and architecture.
These are standalone packages; updates require downloading a new release.

Once the package repositories are published, install on macOS or Linux with Homebrew:

```sh
brew install renatko/tap/ch-insp
```

On Windows with Scoop:

```powershell
scoop bucket add renatko https://github.com/renatko/scoop-bucket
scoop install renatko/ch-insp
```

The executable is named `ch_insp`. To update it, use `brew upgrade ch-insp` or
`scoop update ch-insp`.

To build from source, install Go 1.27.3 or newer:

```sh
git clone https://github.com/renatko/ch_insp.git
cd ch_insp
go build -o ch_insp .
```

## Quick start

Connect to ClickHouse using a URL or command-line options:

```sh
export CLICKHOUSE_URL='clickhouse://default@localhost:9000/demo'
./ch_insp
```

Or pass connection options explicitly:

```sh
./ch_insp --host localhost --port 9000 --user default --database demo
```

For credentials, prefer the environment variable over command-line arguments, which may be visible in process listings and shell history. See [connection options](docs/usage.md#connecting-and-offline-mode) for TLS, precedence and defaults.

To explore a local SQL dump without a ClickHouse server, use `./ch_insp --schema /path/to/schema.sql`. From a source checkout, you can try the included example:

```sh
go run . --schema ./demo/schema.sql
```

The dump is read locally; its statements are not executed. Offline mode does not provide live metrics, execution errors or node switching. See [offline mode](docs/usage.md#connecting-and-offline-mode).

## Try the demo

From a source checkout with Docker Compose installed:

```sh
docker compose -f demo/compose.yaml up -d --wait
docker compose -f demo/compose.yaml --profile tui run --build --rm tui
```

The demo provides a standalone server and a two-replica cluster with synthetic traffic. To stop it, run `docker compose -f demo/compose.yaml down`; demo data stored in its containers is discarded. See [demo setup and verification](docs/demo.md).

## Basic controls

| Key | Action |
| --- | --- |
| `↑` / `↓`, `←` / `→` | Navigate flows and targets |
| `Enter` | Open the selected MV or target |
| `f` / `/` | Filter or search flows |
| `t` / `g` | Inspect a target or table dependencies |
| `c` / `d` | Open column flow or SQL from the MV detail |
| `e` / `w` | Inspect observed MV errors / change error window |
| `p` / `s` | Change chart period / scale |
| `b` / `n` | Switch database / inspect nodes |
| `Esc` / `q` | Go back / quit |

Mouse selection, clicking and scrolling are also supported. See the [complete controls](docs/usage.md#keyboard-and-mouse).

## Reading the overview

`[T]` marks an inferred insert trigger and `[I]` another query input; these roles and column lineage are derived from SQL, not observed writes. `R` and `W` are MV read/write rates; target `total` includes observed writers. Charts use five completed intervals: `0` means an event reported zero rows, while `·` means no event was observed.

Access is read-only. Metrics are local to the connected node and depend on ClickHouse system logs, which can be delayed, disabled or sampled; missing events do not prove inactivity. See [metrics and node surveys](docs/metrics.md) and [lineage limitations](docs/lineage.md).

## Documentation

- [Usage, connection options and navigation](docs/usage.md)
- [Metrics, logs and cluster surveys](docs/metrics.md)
- [Target columns and column lineage](docs/lineage.md)
- [Docker demo](docs/demo.md)
- [Development, tests and release artifacts](docs/development.md)

## License

[MIT](LICENSE)
