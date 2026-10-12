package main

import (
	"math"
	"testing"
)

func TestTwoPositionsTPAndBreakeven(t *testing.T) {
	s := &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4090}
	bars := []Candle{{Time: 180, Open: 4100, High: 4101, Low: 4099, Close: 4101}, {Time: 360, Open: 4102, High: 4106, Low: 4101, Close: 4105}, {Time: 540, Open: 4104, High: 4111, Low: 4101, Close: 4110}}
	tr, _ := simulateTrade(s, bars, BacktestCosts{ContractSize: 100})
	if tr.Legs[0].Lots != .1 || tr.Legs[1].Lots != .1 || tr.Legs[0].Profit != 50 || tr.Legs[1].Profit != 100 {
		t.Fatalf("wrong pair: %+v", tr)
	}
	bars[2] = Candle{Time: 540, Open: 4104, High: 4105, Low: 4099, Close: 4100}
	tr, _ = simulateTrade(s, bars, BacktestCosts{ContractSize: 100})
	if tr.Legs[1].Reason != "breakeven" || tr.Legs[1].Exit != 4100 {
		t.Fatalf("bad BE: %+v", tr)
	}
}
func TestAmbiguousStopFirst(t *testing.T) {
	tr, _ := simulateTrade(&Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4090}, []Candle{{Time: 180, Open: 4100, High: 4115, Low: 4080, Close: 4110}}, BacktestCosts{ContractSize: 100})
	if !tr.Ambiguous || tr.Legs[0].Reason != "stop" || tr.Legs[1].Reason != "stop" {
		t.Fatal(tr)
	}
}
func TestSellCostsAndGap(t *testing.T) {
	tr, _ := simulateTrade(&Signal{TP1: 4095, TP2: 4090, Side: "SELL", Stop: 4110}, []Candle{{Time: 180, Open: 4100, High: 4101, Low: 4099, Close: 4100}, {Time: 360, Open: 4115, High: 4116, Low: 4114, Close: 4115}}, BacktestCosts{Spread: .2, Slippage: .1, ContractSize: 100, Commission: 7})
	if math.Abs(tr.Legs[0].Entry-4099.9) > 1e-8 || math.Abs(tr.Legs[0].Exit-4115.3) > 1e-8 || math.Abs(tr.Legs[0].Profit-(-154.7)) > 1e-6 {
		t.Fatal(tr)
	}
}
func TestTP1SameBarBE(t *testing.T) {
	tr, _ := simulateTrade(&Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4090}, []Candle{{Time: 180, Open: 4100, High: 4111, Low: 4099, Close: 4110}}, BacktestCosts{ContractSize: 100})
	if tr.Legs[0].Reason != "tp1" || tr.Legs[1].Reason != "breakeven" || !tr.Ambiguous {
		t.Fatal(tr)
	}
}
