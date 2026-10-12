# Research tools

These commands do not send broker orders. Cost values below are illustrative;
verify contract specifications and execution assumptions before interpreting results.

## Candle history

Compile `mt5/GoldHistoryExport.mq5` under MT5 Scripts and run it on the demo account.
It requests 50,000 closed M3 bars and writes `MQL5/Files/GoldHistory-TIMESTAMP.json`.
Partial history is reported; let MT5 synchronize and rerun when needed.

```sh
go run . import-history /path/to/GoldHistory-TIMESTAMP.json YourBroker-Demo
go run . backtest -spread 0.24 -slippage 0.05 -commission 0 -contract-size 100 -output .local/backtest.json
```

The importer inserts missing candles without changing live freshness or generating
signals. The database assumes one broker feed; do not mix servers. Timestamps are
not normalized to UTC. The legacy OHLC simulator retains 0.10 lots per leg and
conservative intrabar assumptions; it is not an exact model of the executor.

## Tick replay

Compile `mt5/GoldTickExport.mq5` under Scripts. It exports seven days by default
(configurable 1–31) into `MQL5/Files/GoldTicks-TIMESTAMP.jsonl`, with metadata and a
completion footer. The simulator rejects partial, failed, cancelled, unordered, or
mismatched exports. Large quote gaps reset signal warm-up, but do not prove why
history is missing.

```sh
go run . tick-backtest -file /path/to/GoldTicks-TIMESTAMP.jsonl -server YourBroker-Demo -balance 10000 -contract-size 100 -slippage 0.05 -commission 0 -margin-per-lot 1000 -stopout-percent 0 -stop-distance 0.10 -entry-delay-ms 200 -modify-delay-ms 200 -output .local/tick-backtest.json
```

Defaults: two 0.06-lot legs, $200 pair risk, $1,000 daily equity gate, no minimum
reward/risk filter by default. Configure with `-lots`, `-max-pair-risk`,
`-daily-loss-limit`, and `-min-reward-risk`. Zero disables an individual research
risk filter. Both lookbacks (10 and 20) are compared.

Replay uses bid/ask quotes, delayed entries and break-even modifications. It still
uses assumed slippage, fees, margin, and stop-out behavior. Day boundaries follow
raw export timestamps, not a verified Lagos conversion. Neither replay engine
reproduces all broker failures. Keep raw exports and generated reports in `.local`.

TP1 must be at least 5.0 gold price units (50 pips by this project’s convention)
from the reference entry. Select the nearest eligible confirmed swing, then the
next farther distinct swing for TP2. Skip if two eligible levels are unavailable.
Replay checks the simulated fill; the executor checks the current quote and actual
fill. A fill violating the minimum invokes incomplete-pair recovery.
