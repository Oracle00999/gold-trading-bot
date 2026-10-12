# $10,000 demo configuration replay

Starting balance $10,000; two 0.06-lot positions; $200 pair-risk ceiling; $1,000 daily equity loss gate; nearest confirmed swing stop; 1:1 combined full-target reward/risk filter retained. Research only, no broker balance change or order execution.

Assumptions: recorded bid/ask, 0.05 adverse slippage, zero commission, 200 ms entry/modification delays, fixed $1,000 margin per lot, 0% stop-out, 0.10 minimum stop distance. Same seven-day export of 1,332,222 ticks; not independent validation.

| Lookback | Closed legs | Open legs | Ending equity | Maximum equity drawdown |
|---|---:|---:|---:|---:|
| 10 | 168 | 2 | $8993.42 | $1280.43 |
| 20 | 118 | 0 | $9276.35 | $869.75 |

The retained reward filter limits estimated risk to about $90 before costs with current targets/lots, below the $200 ceiling. Daily limits block new entries rather than force closing existing positions and cannot guarantee a hard maximum loss during gaps.
