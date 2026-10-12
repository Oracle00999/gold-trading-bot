# Gold Trading Bot

A personal, demo-only XAUUSDm trading system built with Go, Gin, PostgreSQL, and
MetaTrader 5. The dashboard shows live quotes, candles, strategy signals, and
execution status. GoldBridge supplies market data; GoldExecutor places and manages
orders on an explicitly configured USD hedging demo account.

## Architecture

- **Go + Gin:** dashboard, closed-candle analysis, authenticated bridge endpoints.
- **PostgreSQL:** candles, strategy observations, execution snapshots, deals and events.
- **GoldBridge:** uploads quotes and M3/M5/M15/H1 candles from MT5.
- **GoldExecutor:** validates signals against MT5 data, checks risk and margin, then
  submits two protected positions. Attach it to a separate chart from GoldBridge.

## Local setup

Requirements: Go matching `go.mod`, Python 3, Docker Compose, and MT5 with MetaEditor.
Run commands from the project root.

```sh
python3 scripts/setup-db.py
docker compose --env-file .local/postgres.env up -d --wait
```

Configure your demo account locally (replace the example values):

```sh
export DEMO_LOGIN=12345678
export DEMO_SERVER=YourBroker-Demo
go run .
```

Alternatively create gitignored `.local/execution-account.json`:

```json
{"account":"12345678","server":"YourBroker-Demo"}
```

Environment configuration takes precedence and must provide both values. Missing
or invalid configuration fails closed. `DATABASE_URL` overrides the generated
`.local/database-url`. Migrations run on startup. PostgreSQL must be available.

Open http://127.0.0.1:8080. Refresh after restarting the app to renew the dashboard's
read-only session. The app binds to loopback; PostgreSQL binds to `127.0.0.1:55432`.
The bridge token is generated in `.local/bridge-token`. Never commit `.local`,
credentials, MT5 presets, database dumps, or raw account exports.

## MT5 setup

1. Copy `mt5/GoldBridge.mq5`, `mt5/GoldExecutor.mq5`, and `mt5/ExecutionPolicy.mqh`
   into MT5's `MQL5/Experts` directory. Compile both EAs in MetaEditor.
2. Allow WebRequest to `http://127.0.0.1:8080` in Tools → Options → Experts.
3. Attach GoldBridge to one XAUUSDm chart and set its BridgeToken from the local file.
4. Attach GoldExecutor to a **second** XAUUSDm chart. Set the same token and set
   DemoLogin/DemoServer to match the Go configuration. Its account defaults are
   deliberately empty and entries default off.
5. Enable `EnableEntries` and MT5 Algo Trading when ready to test demo execution.

Only one executor terminal may run per account. The duplicate-instance lock covers
one terminal, not multiple computers. Reload an EA after compiling an update;
restarting Go does not reload MT5. MT5 and Go must remain running and connected.

## Strategy and risk

Analysis uses closed M3 candles only. A breakout requires a close beyond the
highest high or lowest low of the preceding window, excluding the signal candle.
`BREAKOUT_LOOKBACK` accepts 10 or 20 (default 20). `BREAKOUT_STOP_BUFFER` defaults to
0.10 price units and must be positive.

The stop uses the most recent confirmed three-candle swing low for buys or swing
high for sells, plus an outward buffer. Targets are the nearest two distinct
confirmed swing prices beyond the signal close, found within the latest 200 closed
bars. Missing levels or gaps cause a skip; there is no fixed-distance fallback.
Targets may be close together because prices are distinct, not grouped into zones.

- Two positions of **0.06 lots** each per accepted signal.
- Maximum **$200 estimated combined stop risk**.
- **$1,000 daily equity loss gate**; blocks new entries, does not force-close a
  completed pair. Broker-day equity includes manual activity and cash movements.
- Minimum combined full-target net reward/risk of **1:1**.
- After the full TP1 position closes by broker take-profit, TP2's stop moves to its
  actual entry. This does not guarantee net break-even after fees or slippage.

The executor independently validates data, quote freshness, broker limits, margin,
and configured cost estimates. It waits for a new M3 bar after attachment and never
blindly retries an entry. Any account position or pending order blocks a new pair.
Risk budgets are estimates: gaps and slippage can exceed them.

## Execution recovery and monitoring

Both legs include broker SL/TP, but sequential fills cannot be atomic. An incomplete
or uncertain attempt latches a halt. After a delay, recovery reconciles broker
history and closes only positively identified positions from that incomplete
attempt when permissions allow. It never automatically resumes entries, and leaves
uncertain or pending outcomes for manual review. Completed pairs survive restarts.

State persists in MT5 global variables under `GoldExec.<server>.<login>.`. After a
failed attempt, disable entries and reconcile positions, pending orders, and deals.
Only after confirming the outcome, reset `recovery` and `halt` to zero. Preserve
`attempt` and daily state. Do not reset recovery with unresolved bot orders.

The executor reports every five seconds; after 15 seconds its heartbeat is stale.
GoldBridge freshness is separate. PostgreSQL deduplicates deals/events and restores
history without marking an old heartbeat fresh. Recent deal uploads cover at most
200 matching deals over seven broker days, not an all-time account ledger. The
local event outbox retains up to 50 events; overflow remains in terminal logs.

The dashboard cookie permits reads only. Bridge writes and `/api/bridge/intent`
require the bearer token. Keep the service private; public dashboard deployment
needs additional authentication and access controls.

## Testing

Keep test files in Git: Go excludes `_test.go` files from production builds.

```sh
go test -race ./...
go vet ./...
TEST_POSTGRES=1 go test -run 'TestPostgres|TestExecutionPersistence' -v ./...
c++ -std=c++11 tests/execution_policy_test.cpp -o /tmp/fx-execution-policy-test
/tmp/fx-execution-policy-test
```

The database tests use isolated rows and clean them up. Compile MQL5 separately in
MetaEditor. The C++ test exercises recovery policy shared with MQL5, not a broker
emulator. Demo verification of partial fills, rejected closes, restarts, and TP1
stop management is still necessary before unattended operation.

Build the backend with `go build -o bin/fx-trader .`.

## Research and backups

See [research tools](docs/research.md) for history exports and replay commands.
[Reports](reports/) preserve earlier experiments; they are historical results,
not evidence of current profitability. Raw broker exports stay local.

```sh
mkdir -p .local/backups
docker compose --env-file .local/postgres.env exec -T postgres pg_dump -U fx_trader -d fx_trader -Fc > .local/backups/fx-trader.dump
```

Rename backups to preserve previous versions. `docker compose stop` preserves the
volume; `down -v` deletes it. Volumes are not backups. Restore and reboot testing
are part of deployment. Stop the old executor before enabling one on a VPS.
