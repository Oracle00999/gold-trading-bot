package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"math"
	"os"
	"time"
)

type HistoryExport struct {
	Demo           bool     `json:"demo"`
	Symbol         string   `json:"symbol"`
	Server         string   `json:"server"`
	Timeframe      string   `json:"timeframe"`
	Requested      int      `json:"requested_bars"`
	CurrentBar     int64    `json:"current_bar_time"`
	Contract       float64  `json:"contract_size"`
	VolumeMin      float64  `json:"volume_min"`
	VolumeMax      float64  `json:"volume_max"`
	VolumeStep     float64  `json:"volume_step"`
	TickSize       float64  `json:"tick_size"`
	ProfitCurrency string   `json:"profit_currency"`
	Candles        []Candle `json:"candles"`
}

func parseHistory(r io.Reader) (HistoryExport, error) {
	var h HistoryExport
	d := json.NewDecoder(io.LimitReader(r, 32<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&h); err != nil {
		return h, err
	}
	if d.Decode(new(any)) != io.EOF {
		return h, fmt.Errorf("unexpected trailing data")
	}
	if !h.Demo || h.Symbol != "XAUUSDm" || h.Timeframe != "M3" || h.Server == "" || len(h.Candles) < 22 || len(h.Candles) > 100000 || h.ProfitCurrency != "USD" {
		return h, fmt.Errorf("expected demo XAUUSDm M3 USD export with 22–100000 candles")
	}
	for _, v := range []float64{h.Contract, h.VolumeMin, h.VolumeMax, h.VolumeStep, h.TickSize} {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return h, fmt.Errorf("invalid contract specifications")
		}
	}
	if h.VolumeMin > .1 || h.VolumeMax < .1 || math.Abs(.1/h.VolumeStep-math.Round(.1/h.VolumeStep)) > 1e-7 {
		return h, fmt.Errorf("0.10 lot is incompatible with exported volume limits")
	}
	var previous int64
	for _, c := range h.Candles {
		for _, v := range []float64{c.Open, c.High, c.Low, c.Close} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return h, fmt.Errorf("nonfinite candle")
			}
		}
		if c.Time <= previous {
			return h, fmt.Errorf("duplicate or unordered candle")
		}
		previous = c.Time
		b := CandleBatch{Demo: true, Symbol: h.Symbol, Timeframe: h.Timeframe, CurrentBarTime: h.CurrentBar, Candles: []Candle{c}}
		if err := b.validate(); err != nil {
			return h, err
		}
	}
	return h, nil
}
func importHistoryCLI() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: fx-trader import-history FILE EXPECTED_SERVER")
	}
	f, err := os.Open(os.Args[2])
	if err != nil {
		return err
	}
	defer f.Close()
	h, err := parseHistory(f)
	if err != nil {
		return err
	}
	if h.Server != os.Args[3] {
		return fmt.Errorf("export server differs from expected feed; refusing to mix histories")
	}
	db, err := openDatabase()
	if err != nil {
		return err
	}
	defer db.pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	inserted := int64(0)
	for start := 0; start < len(h.Candles); start += 500 {
		end := start + 500
		if end > len(h.Candles) {
			end = len(h.Candles)
		}
		batch := &pgx.Batch{}
		for _, c := range h.Candles[start:end] {
			batch.Queue(`INSERT INTO candles VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, h.Symbol, h.Timeframe, c.Time, c.Open, c.High, c.Low, c.Close, c.TickVolume)
		}
		results := tx.SendBatch(ctx, batch)
		for i := start; i < end; i++ {
			tag, e := results.Exec()
			if e != nil {
				results.Close()
				return e
			}
			inserted += tag.RowsAffected()
		}
		if err = results.Close(); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("Validated %d/%d requested bars; inserted %d new candles. Existing live candles and freshness unchanged.\n", len(h.Candles), h.Requested, inserted)
	fmt.Printf("Export server: %s; contract size: %g; profit currency: %s; 0.10 lot supported. Source file is the specifications record.\n", h.Server, h.Contract, h.ProfitCurrency)
	return nil
}
