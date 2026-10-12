# Seven-day tick replay review

Research only; no order execution enabled. Starting simulated balance: $1,000. Two separate 0.10-lot positions per signal; TP1 price distance 5, TP2 distance 10; move leg 2 stop to its entry after TP1. Structural stop uses the opposite rolling extreme with a 0.10 buffer. Signals use closed M3 bid candles.

## Export validation

Source: `GoldTicks-1791533368.jsonl`, Exness-MT5Trial9 demo, XAUUSDm. 1,332,222 tick records. Completion footer reports zero failed chunks and no cancellation. Replay validated metadata, quote ordering, quote ranges and footer count. Five quote gaps exceeded 60 seconds; this does not establish whether they were scheduled closures or missing data. Signal history resets after those gaps; existing positions remain exposed.

SHA-256: `4962e89599eedd676074705cff77f747c8d1aac038fa5876864554dab3afe18d`

Export epoch bounds: 1790924969000–1791529769000 milliseconds (seven days). Broker timestamp timezone interpretation has not been independently verified.

## Controlled cost comparison

All rows use 200 ms entry and break-even modification delays. Fixed assumed margin is $1,000 per lot ($200 for the pair), stop-out threshold 0%, minimum stop distance 0.10, contract size 100 units/lot. These margin, stop-out, cost and latency values are scenario inputs, not verified historical broker conditions. Recorded bid/ask spread is used in every scenario.

| Scenario | Lookback | Entered pairs | Ending balance/equity | Maximum equity drawdown |
|---|---:|---:|---:|---:|
| Recorded spread, no extra slippage/commission | 10 | 58 | $93.25 | $1662.08 |
| Recorded spread, no extra slippage/commission | 20 | 38 | $138.04 | $1652.59 |
| Recorded spread + 0.05 slippage, zero commission | 10 | 58 | $49.06 | $1688.42 |
| Recorded spread + 0.05 slippage, zero commission | 20 | 38 | $114.81 | $1663.59 |
| Recorded spread + 0.10 slippage, $7/lot round-trip commission | 10 | 58 | $-4.25 | $1691.03 |
| Recorded spread + 0.10 slippage, $7/lot round-trip commission | 20 | 38 | $32.27 | $1686.49 |

No positions remain open in these results. Drawdown is measured from the running equity peak, so it can exceed starting capital. The approximate stop-out model can produce a small negative balance; this does not model broker negative-balance protection.

## Findings and limits

Both lookbacks lost heavily in this sample, including with no added commission or slippage. In the base 10-candle run, the first pair lost about $280.22 and another early pair lost $383.28, versus $150 gross if both targets hit. Wide structural stops combined with fixed lot sizes expose a large fraction of a $1,000 account per signal.

Base cases entered 58 pairs (10 candles) and 38 pairs (20 candles); 108 and 72 subsequent signals were rejected. Rejection totals combine margin and other validation failures; they are not all classified as insufficient margin.

A separate zero-delay scenario (`.local/tick-zero_cost.json`) follows a different trade path and exhausts the account sooner. It must not be interpreted as evidence that lower costs worsen performance: both latency and costs changed. The controlled comparison above holds delays constant.

This is one seven-day sample, not an out-of-sample strategy validation. No swaps, historical dynamic margin, separate leg execution failures, partial fills, or actual broker stop/freeze restrictions are reproduced. Tick export completion does not guarantee complete broker history. Tick chronology improves on candle-only ordering assumptions but is still a simulation.

Next proposed research change: make maximum cash risk per pair explicit and skip signals whose structural stop exceeds that budget, retaining the requested two 0.10-lot positions. Choose a risk budget before implementing a trading rule; do not silently tighten structural stops or change lots. Then compare on additional independent periods. No evidence here supports enabling real-account trading.

Raw reports are in `.local/tick-base.json`, `.local/tick-no_added_cost.json`, and `.local/tick-cost_only_stress.json`.
