package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"time"
)

type BacktestCosts struct {
	Spread       float64 `json:"spread_price"`
	Slippage     float64 `json:"slippage_price_per_market_fill"`
	ContractSize float64 `json:"units_per_lot"`
	Commission   float64 `json:"round_trip_commission_per_lot"`
}
type BacktestLeg struct {
	Entry    float64 `json:"entry"`
	Exit     float64 `json:"exit"`
	Lots     float64 `json:"lots"`
	Reason   string  `json:"reason"`
	ExitTime int64   `json:"exit_time"`
	Profit   float64 `json:"profit_quote_currency"`
}
type BacktestTrade struct {
	SignalTime int64          `json:"signal_time"`
	EntryTime  int64          `json:"entry_time"`
	Side       string         `json:"side"`
	Stop       float64        `json:"initial_stop"`
	Legs       [2]BacktestLeg `json:"positions"`
	Ambiguous  bool           `json:"ambiguous_intrabar"`
}
type BacktestReport struct {
	Config              StrategyConfig  `json:"strategy"`
	Costs               BacktestCosts   `json:"cost_assumptions"`
	Bars                int             `json:"bars"`
	Gaps                int             `json:"gaps"`
	Net                 float64         `json:"net_quote_currency"`
	MaxDrawdown         float64         `json:"max_closed_signal_drawdown"`
	LongestLosingStreak int             `json:"longest_losing_streak"`
	Wins                int             `json:"profitable_signals"`
	Trades              []BacktestTrade `json:"trades"`
	Notes               []string        `json:"limitations"`
}

func simulateTrade(s *Signal, bars []Candle, cost BacktestCosts) (BacktestTrade, int) {
	direction := 1.0
	if s.Side == "SELL" {
		direction = -1
	}
	entry := bars[0].Open
	if direction > 0 {
		entry += cost.Spread
	}
	entry += direction * cost.Slippage
	tr := BacktestTrade{SignalTime: s.CandleTime, EntryTime: bars[0].Time, Side: s.Side, Stop: s.Stop}
	for i := range tr.Legs {
		tr.Legs[i] = BacktestLeg{Entry: entry, Lots: .1}
	}
	stop := s.Stop
	closeLeg := func(i int, price float64, reason string, at int64) {
		l := &tr.Legs[i]
		l.Exit = price
		l.Reason = reason
		l.ExitTime = at
		l.Profit = (price-entry)*direction*.1*cost.ContractSize - .1*cost.Commission
	}
	for idx, b := range bars {
		// MT5 FX/CFD candle history is treated as bid OHLC; synthesize ask for sell exits.
		offset := 0.0
		if direction < 0 {
			offset = cost.Spread
		}
		o, h, l := b.Open+offset, b.High+offset, b.Low+offset
		stopped := func(p float64) bool { return direction > 0 && l <= p || direction < 0 && h >= p }
		targetHit := func(p float64) bool { return direction > 0 && h >= p || direction < 0 && l <= p }
		stopFill := func(p float64) float64 {
			if direction > 0 {
				return math.Min(p, o) - cost.Slippage
			}
			return math.Max(p, o) + cost.Slippage
		}
		tp1, tp2 := s.TP1, s.TP2
		// Adverse stop-first ordering when a bar spans both stop and target.
		if stopped(stop) {
			if (tr.Legs[0].Reason == "" && targetHit(tp1)) || targetHit(tp2) {
				tr.Ambiguous = true
			}
			for i := range tr.Legs {
				if tr.Legs[i].Reason == "" {
					reason := "stop"
					if i == 1 && tr.Legs[0].Reason == "tp1" {
						reason = "breakeven"
					}
					closeLeg(i, stopFill(stop), reason, b.Time)
				}
			}
		} else {
			if tr.Legs[0].Reason == "" && targetHit(tp1) {
				closeLeg(0, tp1, "tp1", b.Time)
				stop = entry
				if stopped(stop) {
					tr.Ambiguous = true
					closeLeg(1, entry-direction*cost.Slippage, "breakeven", b.Time)
				}
			}
			if tr.Legs[1].Reason == "" && targetHit(tp2) {
				closeLeg(1, tp2, "tp2", b.Time)
			}
		}
		if tr.Legs[0].Reason != "" && tr.Legs[1].Reason != "" {
			return tr, idx
		}
	}
	last := bars[len(bars)-1]
	price := last.Close
	if direction < 0 {
		price += cost.Spread
	}
	price -= direction * cost.Slippage
	for i := range tr.Legs {
		if tr.Legs[i].Reason == "" {
			closeLeg(i, price, "end_of_data", last.Time)
		}
	}
	return tr, len(bars) - 1
}
func backtest(bars []Candle, cfg StrategyConfig, cost BacktestCosts) BacktestReport {
	r := BacktestReport{Config: cfg, Costs: cost, Bars: len(bars), Trades: []BacktestTrade{}, Notes: []string{"Research simulation, not broker execution or a profitability guarantee.", "Two 0.10-lot positions; one pair active at a time. Next contiguous candle open entry.", "Fixed spread/slippage; no swaps, margin checks, latency, or failed/partial orders modeled.", "Ambiguous OHLC bars use stop-first ordering; TP1 and possible breakeven in the same candle use breakeven first.", "Remaining positions close at end of data. Drawdown uses completed signal P&L, not intratrade equity.", "Results are in quote currency (USD for XAUUSD); contract size and costs are explicit user assumptions."}}
	peak, streak := 0.0, 0
	for i := 1; i < len(bars); i++ {
		if bars[i].Time-bars[i-1].Time != 180 {
			r.Gaps++
		}
	}
	for i := cfg.Lookback; i < len(bars)-1; i++ {
		if bars[i+1].Time-bars[i].Time != 180 {
			continue
		}
		s, _ := breakout(CandleBatch{Symbol: "XAUUSDm", Timeframe: "M3", CurrentBarTime: bars[i].Time + 180, Candles: bars[:i+1]}, cfg)
		if s == nil {
			continue
		}
		entry := bars[i+1].Open
		if s.Side == "BUY" {
			entry += cost.Spread + cost.Slippage
		} else {
			entry -= cost.Slippage
		}
		if (s.Side == "BUY" && (entry <= s.Stop || entry >= s.TP1)) || (s.Side == "SELL" && (entry >= s.Stop || entry <= s.TP1)) {
			continue
		}
		tr, used := simulateTrade(s, bars[i+1:], cost)
		r.Trades = append(r.Trades, tr)
		profit := tr.Legs[0].Profit + tr.Legs[1].Profit
		r.Net += profit
		peak = math.Max(peak, r.Net)
		r.MaxDrawdown = math.Max(r.MaxDrawdown, peak-r.Net)
		if profit > 0 {
			r.Wins++
			streak = 0
		} else if profit < 0 {
			streak++
			if streak > r.LongestLosingStreak {
				r.LongestLosingStreak = streak
			}
		} else {
			streak = 0
		}
		// Can evaluate the exit bar's close, but cannot enter before the next bar.
		i += used
	}
	return r
}
func runBacktestCLI() error {
	f := flag.NewFlagSet("backtest", flag.ContinueOnError)
	spread := f.Float64("spread", -1, "assumed ask-bid price distance")
	slip := f.Float64("slippage", -1, "adverse price slippage per market fill")
	contract := f.Float64("contract-size", 0, "broker units per lot; verify in MT5 specifications")
	commission := f.Float64("commission", -1, "round-trip commission per lot in USD")
	output := f.String("output", ".local/backtest.json", "JSON report path")
	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}
	for _, v := range []float64{*spread, *slip, *commission, *contract} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("provide finite nonnegative spread, slippage, commission and positive contract-size")
		}
	}
	if *contract <= 0 {
		return fmt.Errorf("contract-size must be positive; verify broker specifications")
	}
	db, err := openDatabase()
	if err != nil {
		return err
	}
	defer db.pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := db.pool.Query(ctx, "SELECT broker_time,open,high,low,close,tick_volume FROM candles WHERE symbol='XAUUSDm' AND timeframe='M3' ORDER BY broker_time")
	if err != nil {
		return err
	}
	defer rows.Close()
	var bars []Candle
	for rows.Next() {
		var b Candle
		if err = rows.Scan(&b.Time, &b.Open, &b.High, &b.Low, &b.Close, &b.TickVolume); err != nil {
			return err
		}
		bars = append(bars, b)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(bars) < 22 {
		return fmt.Errorf("need at least 22 closed M3 candles; have %d", len(bars))
	}
	cfg, err := strategyConfig()
	if err != nil {
		return err
	}
	cost := BacktestCosts{*spread, *slip, *contract, *commission}
	reports := []BacktestReport{}
	for _, n := range []int{10, 20} {
		cfg.Lookback = n
		reports = append(reports, backtest(bars, cfg, cost))
	}
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(*output, data, 0600); err != nil {
		return err
	}
	fmt.Printf("Saved comparison of 10/20 lookbacks using %d candles to %s. Results are exploratory; inspect assumptions and sample coverage.\n", len(bars), *output)
	return nil
}
