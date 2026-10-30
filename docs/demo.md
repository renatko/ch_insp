# Docker demo

Run these commands from the root of a source checkout.

The demo starts a standalone ClickHouse server and a separate one-shard, two-replica cluster, using the pinned ClickHouse **26.8.36.3 LTS** image. A traffic generator continuously inserts synthetic events into both environments, varying the number of inserts, rows per insert and intervals between them. Docker Compose and access to the container images are required.

```sh
docker compose -f demo/compose.yaml up -d --wait
```

Open the standalone server from the host:

```sh
go run . --host localhost --port 19100 --database demo
```

For the cluster, run the TUI on the Docker network so that both advertised node names are reachable. The first run builds the application image:

```sh
docker compose -f demo/compose.yaml --profile tui run --build --rm tui
```

The flow overview opens at startup. Press `n`, select a `demo_cluster` node and press `v` to survey both replicas. The cluster's native ports are also available on the host at `localhost:19101` and `localhost:19102`; when the TUI runs on the host, the Docker-internal node names are not normally resolvable, so use the containerized TUI for node switching and cluster surveys.

The `demo` database has 16 materialized views with branching and shared targets:

- `page_views`, `purchases`, `refunds` and `heartbeat` feed the shared `activity` table. `page_views_mv` also joins the `segments` lookup table.
- `activity` feeds `daily_activity`, then `revenue_daily`; other views select events for `value_alerts` and `audit_events`.
- Purchases and refunds also feed a shared `customer_ledger`, which feeds `ledger_hourly`. Page views feed `page_metrics`; page views and heartbeats also feed `audit_events`, which feeds `audit_summary`.
- The views include filters, joins, expressions and grouped aggregates. Inserts into `heartbeat` use the `Null` engine: they trigger MVs but are not retained, so its `inserted` metric based on `NewPart` is unavailable.

On the standalone server, the other tables use `MergeTree`; in the cluster the event and target tables use `ReplicatedMergeTree`, while each replica has a local `MergeTree` lookup table. Events are generated on the standalone server and both replicas; the MergeTree event tables use a one-day TTL. The default user has no password: this is an isolated local demo, not a production configuration. Native ports bind to loopback, but containers on the Compose network can access the servers.

Verify the running demo, including data and MV log events:

```sh
./demo/scripts/check.sh
```

Stop and remove only the demo containers and network with `docker compose -f demo/compose.yaml down`. Demo data lives in container storage and is discarded when the containers are removed. Ports can be changed with `DEMO_STANDALONE_PORT`, `DEMO_REPLICA1_PORT` and `DEMO_REPLICA2_PORT`; internal cluster addresses do not change.
