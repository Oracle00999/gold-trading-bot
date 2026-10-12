package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
)

type Tick struct {
	Time int64   `json:"time"`
	Bid  float64 `json:"bid"`
	Ask  float64 `json:"ask"`
}
type TickConfig struct {
	Lots, MaxPairRisk, DailyLossLimit, MinRewardRisk float64

	Lookback                                                                            int
	Balance, Contract, Slippage, Commission, MarginPerLot, StopDistance, StopOutPercent float64
	EntryDelay, ModifyDelay, MaxGap                                                     int64
}
type TickPosition struct {
	Lots float64

	Entry, Stop, Target float64
	Direction           float64
	Leg                 int
	Opened              int64
}
type TickExit struct {
	Lots float64

	Leg                 int    `json:"leg"`
	Time                int64  `json:"time_ms"`
	Reason              string `json:"reason"`
	Entry, Exit, Profit float64
}
type TickReport struct {
	RiskRejected, RewardRejected, DailyRejected int

	Config                                             TickConfig
	Ticks, Signals, Rejected, GapCount, ModifyRejected int
	Balance, Equity, MaxEquityDrawdown                 float64
	Exits                                              []TickExit
	OpenPositions                                      int
	Notes                                              []string
}
type tickEngine struct {
	day          int64
	dayStart     float64
	dailyBlocked bool

	cfg                 TickConfig
	r                   TickReport
	positions           []TickPosition
	closed              []Candle
	bar                 Candle
	last                Tick
	pending             *Signal
	entryDue, modifyDue int64
	peak                float64
	discardBar          bool
}

func newTickEngine(c TickConfig) *tickEngine {
	if c.Lots == 0 {
		c.Lots = .1
	}
	return &tickEngine{day: -1, cfg: c, r: TickReport{Config: c, Balance: c.Balance, Exits: []TickExit{}, Notes: []string{"Two simultaneous configured-lot fills at observed bid/ask plus configured slippage; no partial fills.", "Margin-per-lot and stop-out are fixed assumptions, not historical broker margin rules. Commission is round-trip per lot, charged half at entry and exit.", "Risk filters use fixed per-leg lots; daily equity gate resets on export-clock days, blocks new entries only, and cannot guarantee a maximum loss through gaps. Rejection reason counters overlap.", "No swaps. Open positions are marked to market at the end, not assumed closed.", "M3 bid candles reconstructed from ticks; first partial bar discarded. Gaps over configured threshold reset signal history, but existing positions remain exposed.", "Stop modification failures keep original stop and retry on later ticks. Equal-timestamp ticks retain file order."}}, peak: c.Balance, discardBar: true}
}
func (e *tickEngine) equity(t Tick) float64 {
	v := e.r.Balance
	for _, p := range e.positions {
		q := t.Bid
		if p.Direction < 0 {
			q = t.Ask
		}
		v += (q - p.Entry) * p.Direction * p.Lots * e.cfg.Contract
	}
	return v
}
func (e *tickEngine) close(i int, t Tick, price float64, reason string) {
	p := e.positions[i]
	profit := (price-p.Entry)*p.Direction*p.Lots*e.cfg.Contract - p.Lots*e.cfg.Commission/2
	e.r.Balance += profit
	e.r.Exits = append(e.r.Exits, TickExit{p.Lots, p.Leg, t.Time, reason, p.Entry, price, profit})
	e.positions = append(e.positions[:i], e.positions[i+1:]...)
}
func (e *tickEngine) step(t Tick) {
	e.r.Ticks++
	day := t.Time / 86400000
	if day != e.day {
		e.day = day
		e.dayStart = e.equity(t)
		e.dailyBlocked = false
	}
	e.checkDaily(t)
	gap := e.last.Time > 0 && t.Time-e.last.Time > e.cfg.MaxGap
	if gap {
		e.r.GapCount++
		e.closed = nil
		e.pending = nil
		e.bar = Candle{}
		e.discardBar = true
	}
	// Existing broker stops/targets act before any new stop modification.
	for i := 0; i < len(e.positions); {
		p := e.positions[i]
		q := t.Bid
		if p.Direction < 0 {
			q = t.Ask
		}
		if (q-p.Stop)*p.Direction <= 0 {
			reason := "stop"
			if p.Stop == p.Entry {
				reason = "breakeven"
			}
			e.close(i, t, q-p.Direction*e.cfg.Slippage, reason)
			continue
		}
		if (q-p.Target)*p.Direction >= 0 {
			leg := p.Leg
			e.close(i, t, p.Target, fmt.Sprintf("tp%d", leg))
			if leg == 1 {
				e.modifyDue = t.Time + e.cfg.ModifyDelay
			}
			continue
		}
		i++
	}
	if e.modifyDue > 0 && t.Time >= e.modifyDue {
		for i := range e.positions {
			p := &e.positions[i]
			if p.Leg != 2 {
				continue
			}
			q := t.Bid
			if p.Direction < 0 {
				q = t.Ask
			}
			if (q-p.Entry)*p.Direction >= e.cfg.StopDistance && q != p.Entry {
				p.Stop = p.Entry
				e.modifyDue = 0
			} else {
				e.r.ModifyRejected++
			}
		}
		if len(e.positions) == 0 {
			e.modifyDue = 0
		}
	}
	// Approximate account stop-out, explicitly distinct from broker-specific rules.
	if len(e.positions) > 0 && e.equity(t) <= float64(len(e.positions))*e.cfg.Lots*e.cfg.MarginPerLot*e.cfg.StopOutPercent/100 {
		for len(e.positions) > 0 {
			p := e.positions[0]
			q := t.Bid
			if p.Direction < 0 {
				q = t.Ask
			}
			e.close(0, t, q-p.Direction*e.cfg.Slippage, "margin_stopout")
		}
	}
	e.checkDaily(t)
	at := t.Time / 180000 * 180
	if e.bar.Time != at {
		if e.bar.Time > 0 && !e.discardBar {
			e.closed = append(e.closed, e.bar)
			if len(e.closed) > 200 {
				e.closed = e.closed[len(e.closed)-200:]
			}
			if len(e.positions) == 0 && e.pending == nil && at == e.bar.Time+180 {
				s, _ := breakout(CandleBatch{Symbol: "XAUUSDm", Timeframe: "M3", CurrentBarTime: at, Candles: e.closed}, StrategyConfig{e.cfg.Lookback, .1})
				if s != nil {
					e.r.Signals++
					e.pending = s
					e.entryDue = t.Time + e.cfg.EntryDelay
				}
			}
		}
		e.discardBar = e.bar.Time == 0
		if gap {
			e.discardBar = true
		}
		e.bar = Candle{Time: at, Open: t.Bid, High: t.Bid, Low: t.Bid, Close: t.Bid}
	}
	e.bar.High = math.Max(e.bar.High, t.Bid)
	e.bar.Low = math.Min(e.bar.Low, t.Bid)
	e.bar.Close = t.Bid
	e.bar.TickVolume++
	if e.pending != nil && t.Time >= e.entryDue {
		s := e.pending
		e.pending = nil
		d := 1.0
		entry := t.Ask
		if s.Side == "SELL" {
			d = -1
			entry = t.Bid
		}
		entry += d * e.cfg.Slippage
		q := t.Bid
		if d < 0 {
			q = t.Ask
		}
		lots := e.cfg.Lots
		risk := 2 * lots * ((entry-s.Stop)*d*e.cfg.Contract + e.cfg.Slippage*e.cfg.Contract + e.cfg.Commission)
		reward := lots * (((s.TP1-entry)+(s.TP2-entry))*d*e.cfg.Contract - 2*e.cfg.Commission)
		remaining := e.cfg.DailyLossLimit - math.Max(0, e.dayStart-e.equity(t))
		riskBlocked := e.cfg.MaxPairRisk > 0 && risk > e.cfg.MaxPairRisk
		rewardBlocked := e.cfg.MinRewardRisk > 0 && reward < risk*e.cfg.MinRewardRisk
		dailyBlocked := e.cfg.DailyLossLimit > 0 && (e.dailyBlocked || risk > remaining)
		postEquity := e.r.Balance - lots*e.cfg.Commission + (q-entry)*d*2*lots*e.cfg.Contract
		if (s.TP1-entry)*d < minTargetDistance-1e-8 || (s.TP2-s.TP1)*d < 0 || riskBlocked || rewardBlocked || dailyBlocked || len(e.positions) > 0 || t.Time/1000 >= s.CandleTime+360 || (q-s.Stop)*d < e.cfg.StopDistance || (entry-s.Stop)*d <= 0 || postEquity < 2*lots*e.cfg.MarginPerLot || e.r.Balance <= 0 {
			e.r.Rejected++
			if riskBlocked {
				e.r.RiskRejected++
			}
			if rewardBlocked {
				e.r.RewardRejected++
			}
			if dailyBlocked {
				e.r.DailyRejected++
			}
		} else {
			e.modifyDue = 0
			e.r.Balance -= lots * e.cfg.Commission
			for leg := 1; leg <= 2; leg++ {
				e.positions = append(e.positions, TickPosition{lots, entry, s.Stop, []float64{s.TP1, s.TP2}[leg-1], d, leg, t.Time})
			}
		}
	}
	e.last = t
	e.r.Equity = e.equity(t)
	e.peak = math.Max(e.peak, e.r.Equity)
	e.r.MaxEquityDrawdown = math.Max(e.r.MaxEquityDrawdown, e.peak-e.r.Equity)
	e.r.OpenPositions = len(e.positions)
}
func (e *tickEngine) checkDaily(t Tick) {
	if e.cfg.DailyLossLimit > 0 && e.dayStart-e.equity(t) >= e.cfg.DailyLossLimit {
		e.dailyBlocked = true
	}
}

func replayTicks(r io.Reader, c TickConfig, expected string) (TickReport, error) {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), 1<<20)
	e := newTickEngine(c)
	header, end := false, false
	var count int
	var from, to int64
	for scan.Scan() {
		var raw struct {
			Type string `json:"type"`
			Tick
			Symbol, Server  string
			Demo            bool
			From, To        int64
			Contract        float64 `json:"contract_size"`
			Currency        string  `json:"profit_currency"`
			Count, Failures int
			Cancelled       bool
		}
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			return e.r, err
		}
		if end {
			return e.r, fmt.Errorf("data after export footer")
		}
		switch raw.Type {
		case "metadata":
			if header || raw.Symbol != "XAUUSDm" || raw.Server != expected || !raw.Demo || raw.Currency != "USD" || raw.Contract != c.Contract || raw.From <= 0 || raw.To < raw.From {
				return e.r, fmt.Errorf("metadata mismatch")
			}
			header = true
			from, to = raw.From, raw.To
		case "tick":
			t := raw.Tick
			if !header || t.Time < from || t.Time > to || t.Time < e.last.Time || t.Bid <= 0 || t.Ask < t.Bid || math.IsInf(t.Ask, 0) || math.IsInf(t.Bid, 0) {
				return e.r, fmt.Errorf("invalid or unordered quote")
			}
			e.step(t)
			count++
		case "end":
			if !header || raw.Count != count || raw.Failures != 0 || raw.Cancelled || count == 0 {
				return e.r, fmt.Errorf("incomplete tick export; do not backtest")
			}
			end = true
		default:
			return e.r, fmt.Errorf("unknown record")
		}
	}
	if err := scan.Err(); err != nil {
		return e.r, err
	}
	if !end {
		return e.r, fmt.Errorf("missing completion footer")
	}
	return e.r, nil
}
func tickBacktestCLI() error {
	f := flag.NewFlagSet("tick-backtest", flag.ContinueOnError)
	file := f.String("file", "", "tick JSONL file")
	server := f.String("server", "", "expected demo server")
	out := f.String("output", ".local/tick-backtest.json", "report path")
	c := TickConfig{Balance: 1000, Contract: 100, MaxGap: 60000}
	f.Float64Var(&c.Lots, "lots", .06, "fixed lots per position (two positions)")
	f.Float64Var(&c.MaxPairRisk, "max-pair-risk", 200, "maximum estimated USD loss across both legs; 0 disables")
	f.Float64Var(&c.DailyLossLimit, "daily-loss-limit", 1000, "daily equity loss gate in USD; 0 disables")
	f.Float64Var(&c.MinRewardRisk, "min-reward-risk", 0, "combined full-target net reward / estimated stop loss; 0 disables")
	f.Float64Var(&c.Balance, "balance", 10000, "starting USD balance")
	f.Float64Var(&c.Contract, "contract-size", 100, "verified units/lot")
	f.Float64Var(&c.Slippage, "slippage", -1, "adverse price slippage")
	f.Float64Var(&c.Commission, "commission", -1, "roundtrip USD/lot")
	f.Float64Var(&c.MarginPerLot, "margin-per-lot", -1, "assumed USD margin/lot")
	f.Float64Var(&c.StopOutPercent, "stopout-percent", -1, "assumed margin stopout percent")
	f.Float64Var(&c.StopDistance, "stop-distance", -1, "assumed stop/freeze price distance")
	f.Int64Var(&c.EntryDelay, "entry-delay-ms", 200, "entry latency")
	f.Int64Var(&c.ModifyDelay, "modify-delay-ms", 200, "BE modification latency")
	f.Int64Var(&c.MaxGap, "max-gap-ms", 60000, "reset signal history after tick gaps")
	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *file == "" || *server == "" {
		return fmt.Errorf("file and server required")
	}
	for _, v := range []float64{c.Lots, c.MaxPairRisk, c.DailyLossLimit, c.MinRewardRisk, c.Balance, c.Contract, c.Slippage, c.Commission, c.MarginPerLot, c.StopDistance, c.StopOutPercent} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("explicit finite nonnegative cost and margin assumptions required")
		}
	}
	if c.Lots < .01 || c.Lots > 200 || math.Abs(c.Lots/.01-math.Round(c.Lots/.01)) > 1e-8 {
		return fmt.Errorf("lots must be 0.01 to 200 in 0.01 steps for verified symbol specs")
	}
	if c.Balance <= 0 || c.Contract <= 0 || c.MarginPerLot <= 0 || c.EntryDelay < 0 || c.ModifyDelay < 0 || c.MaxGap <= 0 {
		return fmt.Errorf("invalid configuration")
	}
	var reports []TickReport
	for _, n := range []int{10, 20} {
		c.Lookback = n
		input, err := os.Open(*file)
		if err != nil {
			return err
		}
		r, err := replayTicks(input, c, *server)
		input.Close()
		if err != nil {
			return err
		}
		reports = append(reports, r)
	}
	b, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(*out, b, 0600); err != nil {
		return err
	}
	fmt.Println("Tick research reports saved:", *out)
	return nil
}
