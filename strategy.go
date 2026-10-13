package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
)

type StrategyConfig struct {
	Lookback   int     `json:"lookback"`
	StopBuffer float64 `json:"stop_buffer"`
}

func strategyConfig() (StrategyConfig, error) {
	c := StrategyConfig{Lookback: 20, StopBuffer: 2.1}
	var err error
	if v := os.Getenv("BREAKOUT_LOOKBACK"); v != "" {
		c.Lookback, err = strconv.Atoi(v)
		if err != nil {
			return c, err
		}
	}
	if v := os.Getenv("BREAKOUT_STOP_BUFFER"); v != "" {
		c.StopBuffer, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return c, err
		}
	}
	if (c.Lookback != 10 && c.Lookback != 20) || c.StopBuffer <= 0 || math.IsNaN(c.StopBuffer) || math.IsInf(c.StopBuffer, 0) {
		return c, fmt.Errorf("lookback must be 10 or 20 and stop buffer must be finite and positive")
	}
	return c, nil
}

type Signal struct {
	Symbol         string         `json:"symbol"`
	Timeframe      string         `json:"timeframe"`
	CandleTime     int64          `json:"candle_time"`
	Side           string         `json:"side"`
	ReferenceEntry float64        `json:"reference_entry"`
	Support        float64        `json:"support"`
	Resistance     float64        `json:"resistance"`
	Stop           float64        `json:"stop"`
	TP1            float64        `json:"tp1"`
	TP2            float64        `json:"tp2"`
	Config         StrategyConfig `json:"config"`
}

// Evaluate only the latest closed bar, excluding it from the rolling window.
// Targets use the signal close for observation, never an assumed executable fill.
func breakout(b CandleBatch, c StrategyConfig) (*Signal, string) {
	if b.Timeframe != "M3" || len(b.Candles) < c.Lookback+1 {
		return nil, "waiting_for_history"
	}
	bars := b.Candles[len(b.Candles)-c.Lookback-1:]
	last := bars[len(bars)-1]
	if last.Time+180 != b.CurrentBarTime {
		return nil, "incomplete_window"
	}
	for i := 1; i < len(bars); i++ {
		if bars[i].Time-bars[i-1].Time != 180 {
			return nil, "incomplete_window"
		}
	}
	high, low := bars[0].High, bars[0].Low
	for _, bar := range bars[:len(bars)-1] {
		high = math.Max(high, bar.High)
		low = math.Min(low, bar.Low)
	}
	s := &Signal{Symbol: b.Symbol, Timeframe: "M3", CandleTime: last.Time, ReferenceEntry: last.Close, Support: low, Resistance: high, Config: c}
	if last.Close > high {
		s.Side = "BUY"
		s.Stop = nearestSwing(bars, true) - c.StopBuffer
	} else if last.Close < low {
		s.Side = "SELL"
		s.Stop = nearestSwing(bars, false) + c.StopBuffer
	} else {
		return nil, "no_breakout"
	}
	if nearestSwing(bars, s.Side == "BUY") == 0 {
		return nil, "waiting_for_confirmed_swing"
	}
	var ok bool
	s.TP1, s.TP2, ok = structuralTargets(b.Candles, last.Close, s.Side == "BUY")
	if !ok {
		return nil, "waiting_for_structural_targets"
	}
	if s.Stop <= 0 || s.TP2 <= 0 {
		return nil, "invalid_levels"
	}
	return s, "signal_recorded"
}
func (db *candleDatabase) saveSignal(ctx context.Context, s *Signal) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO breakout_signals(symbol,candle_time,lookback,stop_buffer,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, s.Symbol, s.CandleTime, s.Config.Lookback, s.Config.StopBuffer, data)
	return err
}

// Most recent strict three-bar pivot in the closed analysis window. The final
// (breakout) candle can confirm a pivot, but cannot itself be an unconfirmed pivot.
func nearestSwing(bars []Candle, low bool) float64 {
	for i := len(bars) - 2; i >= 1; i-- {
		if low && bars[i].Low < bars[i-1].Low && bars[i].Low < bars[i+1].Low {
			return bars[i].Low
		}
		if !low && bars[i].High > bars[i-1].High && bars[i].High > bars[i+1].High {
			return bars[i].High
		}
	}
	return 0
}

// Nearest two distinct confirmed swing prices beyond entry, not the newest
// two swings. Closed three-bar pivots only; limit history to the bridge's 200.
const minTargetDistance = 5.0

func structuralTargets(bars []Candle, entry float64, buy bool) (float64, float64, bool) {
	if len(bars) > 200 {
		bars = bars[len(bars)-200:]
	}
	first, second := 0.0, 0.0
	for i := 1; i < len(bars)-1; i++ {
		if bars[i].Time-bars[i-1].Time != 180 || bars[i+1].Time-bars[i].Time != 180 {
			continue
		}
		price := bars[i].High
		if buy {
			if price <= bars[i-1].High || price <= bars[i+1].High || price-entry < minTargetDistance-1e-8 {
				continue
			}
		} else {
			price = bars[i].Low
			if price >= bars[i-1].Low || price >= bars[i+1].Low || entry-price < minTargetDistance-1e-8 {
				continue
			}
		}
		if price == first || price == second {
			continue
		}
		if first == 0 || (buy && price < first) || (!buy && price > first) {
			second = first
			first = price
		} else if second == 0 || (buy && price < second) || (!buy && price > second) {
			second = price
		}
	}
	if second == 0 {
		second = first
	}
	return first, second, first > 0
}
