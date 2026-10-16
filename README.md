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
2.10 price units (the original 0.10 plus an extra 2.00) and must be positive.
This applies to new signals; existing positions are not widened.

The stop uses the most recent confirmed three-candle swing low for buys or swing
high for sells, plus an outward buffer. Targets are the nearest two distinct
confirmed swing prices beyond the signal close, found within the latest 200 closed
bars. Both eligible target levels are required; no eligible level, only one level,
or gaps cause a skip. There is no fixed-distance fallback. Targets may be close
together because prices are distinct, not grouped into zones.

- Two positions of **0.06 lots** each per accepted signal.
- Maximum **$200 estimated combined stop risk**.
- **$1,000 daily equity loss gate**; blocks new entries, does not force-close a
  completed pair. Broker-day equity includes manual activity and cash movements.
- The combined estimated net result at TP1 and TP2 must be at least the full
  estimated two-leg stop loss (minimum pair reward/risk **1.0**). It uses the
  broker's profit calculation, configured slippage, and commission estimate.
- After the full TP1 position closes by broker take-profit, TP2's stop moves to its
  actual entry plus $2 for buys or minus $2 for sells (gold price units).
  If broker stop/freeze distance prevents modification, the existing stop stays
  in place and the executor retries. Fees and slippage still affect net profit.
  Historical replay models still use entry-price break-even.

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

The telemetry cookie permits reads only. Bridge writes and `/api/bridge/intent`
require the bearer token. Owner trading controls use a separate authenticated
session through the private dashboard listener; see below.

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

TP1 must be at least 5.0 gold price units (50 pips by this project’s convention)
from the reference entry. Select the nearest eligible confirmed swing, then the
next farther distinct swing for TP2. If only one eligible level exists, both positions use it. Skip only if none exists.
Replay checks the simulated fill; the executor checks the current quote and actual
fill. A fill violating the minimum invokes incomplete-pair recovery.

## Private trading controls

GoldExecutor **1.105** adds a persistent per-symbol new-entry permission. The
initial database setting is **off**. Switching off leaves existing positions,
SL/TP and TP2 profit-lock management running. Switching on waits for a fresh M3
bar; it does not execute a signal seen before the change. API/control failures
block new entries. A pair already being submitted may finish opening both legs;
this switch is not an emergency close button.

The dashboard displays the saved permission and waits for a fresh executor
heartbeat acknowledging its revision. MT5's local EnableEntries, Algo Trading,
daily limits and recovery/halt gates still apply. Use the same executor version
on the server; older versions cannot acknowledge this control.

For owner-only access over Tailscale Serve:

1. Put the exact private HTTPS hostname in `.local/dashboard-host` and the owner's
   exact Tailscale login in `.local/dashboard-owner` (both gitignored).
2. Restart the API. The machine API stays on loopback port 8080. A separate
   dashboard listener runs on **127.0.0.1:8081**.
3. Run `tailscale serve --bg http://127.0.0.1:8081` and open its private HTTPS URL.
   Keep Funnel disabled. Never expose port 8081 through public port mapping.
4. Compile/deploy GoldExecutor 1.105 and refresh the dashboard. Turn Gold on when
   ready; check the executor confirmation.

The control listener trusts Tailscale Serve's sanitized identity headers, and
only the configured owner receives an eight-hour Secure/HttpOnly/SameSite session.
POSTs also require the exact HTTPS Origin and a CSRF token. Direct requests to
8080 cannot obtain a control session or use control routes, even with forged
identity headers or the MT5 bearer token. The private listener denies machine
API routes. As with Tailscale's documented localhost proxy model, **processes on
the VPS itself are trusted**; this is not isolation against a compromised local
administrator. See [Serve identity headers](https://tailscale.com/docs/features/tailscale-serve#identity-headers).

Changes use revision checks to reject concurrent stale updates, and save an
append-only application audit record (`trading_control_audit`) in the same
transaction. Permission survives API, MT5 and VPS restarts. Test with
`TEST_POSTGRES=1 go test -race ./...`; control tests use an isolated schema.

## EURUSD

The pair selector switches between Gold (`XAUUSDm`) and EURUSD (`EURUSDm`),
including their live candles, signals, execution history and owner-only toggle.
EURUSD starts off in the database until its executor is ready and it is enabled.
Gold's existing state and permission are preserved by migration 005.

Both symbols use closed M3 breakouts, confirmed swing stops and two structural
targets; a signal is skipped when a second suitable target is unavailable. EURUSD's
accepted demo settings are: 2-pip stop buffer (0.0002), 5-pip minimum target
(0.0005), 2-pip TP2 profit lock (0.0002), and two 0.06-lot positions. Its slippage
allowance is 0.00001. These starting settings have not been profitability-tested.
Gold retains its existing price distances.

Compile GoldBridge 1.101 and GoldExecutor 1.107. Attach one of each on separate
M3 charts per symbol, setting `GoldSymbol=EURUSDm` on the EUR bridge and
`TradeSymbol=EURUSDm`, `SlippageAllowance=0.00001` on its executor. Keep the same
account pin and bridge URL/token. Do not run another terminal for this account.
The EUR endpoints are under `/api/pairs/EURUSDm`; existing Gold endpoints remain
under `/api`. The executor routes itself based on its TradeSymbol.

Magic numbers, recovery state, event outboxes, execution history and controls
are isolated per symbol. The daily equity baseline and $1,000 daily guard are
shared account-wide. A shared terminal file lock serializes entry attempts;
only one two-position pair may be active across the account. Each symbol still
manages its own positions while the other is polling or attempting entry. An
unresolved recovery on either symbol blocks new entries on both.
