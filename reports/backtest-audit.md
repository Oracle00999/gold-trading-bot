# Gold breakout backtest audit

Audit of the saved 50,000-bar report. Two 0.10-lot positions; contract size 100; assumed spread 0.24, slippage 0.05, commission zero. Results are research P&L, not a funded account simulation.

## Lookback 10

- Signals: 2542; win rate: 73.41%.
- Average winning pair: $78.30; average losing pair: $298.53.
- Implied break-even win rate at those average payoffs: 79.22%. This is descriptive, not a forecast.
- Median initial stop distance: 15.446; 90th percentile: 30.937. Pair dollar risk is roughly distance × $20 before exit slippage/gaps.
- Net: $-55,701.81; ambiguous pairs: 863 (33.9%).
- 859 ambiguous pairs were TP1/breakeven. Hypothetically replacing every one with TP1/TP2 adds $86,329.50, yielding $30,627.69. This is deliberately generous fixed-trade arithmetic, NOT a valid alternative backtest: some never reached TP2, and longer holding times change future entries.

| Exit pair | Count | Net P&L |
|---|---:|---:|
| tp1 / breakeven | 1332 | $65,568.67 |
| tp1 / tp2 | 535 | $80,250.00 |
| stop / stop | 674 | $-201,271.80 |
| end_of_data / end_of_data | 1 | $-248.68 |

Worst pair details (broker timestamps interpreted without timezone conversion):
- BUY, entry 2026-05-29 15:12, entry price 4591.334, initial stop 4531.318, exit 4521.151, pair P&L $-1,403.66.
- BUY, entry 2026-06-24 14:00, entry price 4038.417, initial stop 3971.137, exit 3971.087, pair P&L $-1,346.60.
- SELL, entry 2026-06-17 19:39, entry price 4219.858, initial stop 4279.069, exit 4279.119, pair P&L $-1,185.22.

## Lookback 20

- Signals: 1792; win rate: 78.46%.
- Average winning pair: $77.29; average losing pair: $403.70.
- Implied break-even win rate at those average payoffs: 83.93%. This is descriptive, not a forecast.
- Median initial stop distance: 21.187; 90th percentile: 44.526. Pair dollar risk is roughly distance × $20 before exit slippage/gaps.
- Net: $-47,163.25; ambiguous pairs: 685 (38.2%).
- 682 ambiguous pairs were TP1/breakeven. Hypothetically replacing every one with TP1/TP2 adds $68,541.00, yielding $21,377.75. This is deliberately generous fixed-trade arithmetic, NOT a valid alternative backtest: some never reached TP2, and longer holding times change future entries.

| Exit pair | Count | Net P&L |
|---|---:|---:|
| tp1 / breakeven | 1018 | $50,283.23 |
| tp1 / tp2 | 389 | $58,350.00 |
| stop / stop | 384 | $-155,547.80 |
| end_of_data / end_of_data | 1 | $-248.68 |

Worst pair details (broker timestamps interpreted without timezone conversion):
- BUY, entry 2026-05-29 15:12, entry price 4591.334, initial stop 4525.021, exit 4521.151, pair P&L $-1,403.66.
- BUY, entry 2026-07-01 14:36, entry price 4112.950, initial stop 4044.072, exit 4044.022, pair P&L $-1,378.56.
- SELL, entry 2026-05-18 00:45, entry price 4481.594, initial stop 4544.307, exit 4544.357, pair P&L $-1,255.26.

## Simulator findings

- Signal window excludes the breakout candle; entry is the following candle open. No signal-close fill was used.
- Each leg is 0.10 lots; stop/target direction and bid/ask treatment are internally consistent under the fixed-spread assumption.
- M3 OHLC does not reveal event order. The simulator assumes a stop precedes a target when both are reached, and assumes an entry-price revisit happens AFTER TP1 whenever the same bar contains both. That revisit may actually have happened BEFORE TP1. This is a pessimistic assumption, not an observed sequence.
- Favorable opening gaps are not explicitly processed before intrabar adverse extremes. Some stop-first assignments therefore ignore information available at the opening price. An event-ordered simulator is needed before relying on precise totals.
- 109 non-contiguous intervals appear in the report. These may be session/weekend closures or missing history. Signals reject gaps in their lookback, but positions can span them. Largest loss includes a weekend gap.
- No balance/margin cap, swaps, variable spreads, execution failures, or daily loss limit. Five-month totals cannot describe what a $1,000 account could actually trade.
- No evidence from this audit justifies enabling orders or declaring either lookback profitable.

## Next research steps

1. Replace ambiguous M3 exit ordering with tick data where available (M1 reduces, but does not remove ambiguity); handle opening gaps before intrabar events.
2. Rerun the original unchanged strategy under multiple cost assumptions. Keep an untouched later period for validation.
3. Separately test an explicitly proposed maximum stop-distance/risk filter. Do not silently tighten structural stops or change lot size.
4. Add account balance/margin and daily-loss constraints before any demo execution.
