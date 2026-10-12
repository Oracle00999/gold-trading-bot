package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestHistoryValidation(t *testing.T) {
	h := HistoryExport{Demo: true, Symbol: "XAUUSDm", Server: "demo-test", Timeframe: "M3", Requested: 50, CurrentBar: 5000, Contract: 100, VolumeMin: .01, VolumeMax: 100, VolumeStep: .01, TickSize: .001, ProfitCurrency: "USD"}
	for i := 1; i <= 22; i++ {
		h.Candles = append(h.Candles, Candle{Time: int64(i * 180), Open: 4100, High: 4101, Low: 4099, Close: 4100})
	}
	check := func(h HistoryExport) error {
		b, _ := json.Marshal(h)
		_, err := parseHistory(bytes.NewReader(b))
		return err
	}
	if err := check(h); err != nil {
		t.Fatal(err)
	}
	h.VolumeMin = .2
	if check(h) == nil {
		t.Fatal("invalid lot size allowed")
	}
	h.VolumeMin = .01
	h.Demo = false
	if check(h) == nil {
		t.Fatal("real account allowed")
	}
	h.Demo = true
	h.Candles[21].Time = h.CurrentBar
	if check(h) == nil {
		t.Fatal("forming candle allowed")
	}
}
