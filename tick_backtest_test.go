package main

import (
	"math"
	"strings"
	"testing"
)

func tickFixture() *tickEngine {
	c := TickConfig{Lookback: 10, Balance: 1000, Contract: 100, MarginPerLot: 100, MaxGap: 60000, ModifyDelay: 200, StopDistance: .01}
	e := newTickEngine(c)
	e.positions = []TickPosition{{Lots: .1, Entry: 4100, Stop: 4090, Target: 4105, Direction: 1, Leg: 1}, {Lots: .1, Entry: 4100, Stop: 4090, Target: 4110, Direction: 1, Leg: 2}}
	return e
}
func TestTickSequenceBeforeAndAfterTP1(t *testing.T) {
	e := tickFixture()
	e.step(Tick{1000, 4099, 4099.2})
	e.step(Tick{2000, 4105, 4105.2})
	if len(e.positions) != 1 || e.positions[0].Stop != 4090 {
		t.Fatal("BE applied before delay")
	}
	e.step(Tick{2200, 4104, 4104.2})
	if e.positions[0].Stop != 4100 {
		t.Fatal("BE not applied")
	}
	e.step(Tick{2300, 4112, 4112.2})
	if e.r.Balance != 1150 || len(e.positions) != 0 {
		t.Fatal(e.r)
	}
}
func TestTickDelayedModificationRejection(t *testing.T) {
	e := tickFixture()
	e.step(Tick{1000, 4105, 4105.2})
	e.step(Tick{1200, 4099, 4099.2})
	if e.r.ModifyRejected != 1 || e.positions[0].Stop != 4090 {
		t.Fatal("invalid stop accepted")
	}
	e.step(Tick{1300, 4088, 4088.2})
	if e.r.Exits[1].Reason != "stop" || e.r.Exits[1].Exit != 4088 {
		t.Fatal(e.r)
	}
}
func TestTickAskTriggersShortStop(t *testing.T) {
	e := tickFixture()
	e.positions = []TickPosition{{Lots: .1, Entry: 4100, Stop: 4101, Target: 4095, Direction: -1, Leg: 1}}
	e.step(Tick{1000, 4100.9, 4101.2})
	if len(e.positions) != 0 || e.r.Exits[0].Reason != "stop" {
		t.Fatal(e.r)
	}
}
func TestTickInsufficientMargin(t *testing.T) {
	e := tickFixture()
	e.positions = nil
	e.cfg.MarginPerLot = 10000
	e.pending = &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4090, CandleTime: 0}
	e.step(Tick{1000, 4100, 4100.2})
	if len(e.positions) != 0 || e.r.Rejected != 1 {
		t.Fatal(e.r)
	}
}
func TestTickIncompleteExport(t *testing.T) {
	_, err := replayTicks(strings.NewReader(`{"type":"metadata","symbol":"XAUUSDm","server":"test","demo":true,"from":1,"to":100,"contract_size":100,"profit_currency":"USD"}`), TickConfig{Contract: 100}, "test")
	if err == nil {
		t.Fatal("missing footer accepted")
	}
}
func TestTickInitialPartialBarDiscarded(t *testing.T) {
	e := tickFixture()
	e.positions = nil
	e.step(Tick{190000, 4100, 4100.2})
	e.cfg.MaxGap = 1000000
	e.step(Tick{360000, 4101, 4101.2})
	if len(e.closed) != 0 {
		t.Fatal("partial first candle used")
	}
}

func TestFixedLotsAndRiskGate(t *testing.T) {
	for _, stop := range []float64{4099, 4090} {
		e := newTickEngine(TickConfig{Lots: .06, MaxPairRisk: 20, DailyLossLimit: 100, MinRewardRisk: 1, Balance: 1000, Contract: 100, MarginPerLot: 100, MaxGap: 60000})
		e.pending = &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: stop}
		e.step(Tick{1000, 4100, 4100.2})
		if stop == 4090 {
			if e.r.RiskRejected != 1 || len(e.positions) != 0 {
				t.Fatal(e.r)
			}
			continue
		}
		if len(e.positions) != 2 || e.positions[0].Lots != .06 {
			t.Fatal(e.positions)
		}
		e.step(Tick{2000, 4111, 4111.2})
		if math.Abs(e.r.Balance-1087.6) > 1e-8 {
			t.Fatal(e.r.Balance)
		}
	}
}
func TestDailyGateLatchesAndResets(t *testing.T) {
	e := newTickEngine(TickConfig{Lots: .06, DailyLossLimit: 100, Balance: 1000, Contract: 100, MarginPerLot: 100, MaxGap: 60000})
	e.step(Tick{1000, 4100, 4100.2})
	e.r.Balance = 899
	e.step(Tick{2000, 4100, 4100.2})
	e.r.Balance = 1000 // Recovery must not reopen the gate on the same day.
	e.pending = &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4099}
	e.step(Tick{3000, 4100, 4100.2})
	if e.r.DailyRejected != 1 || len(e.positions) != 0 {
		t.Fatal(e.r)
	}
	e.step(Tick{86401000, 4100, 4100.2})
	e.pending = &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4099, CandleTime: 86400}
	e.step(Tick{86402000, 4100, 4100.2})
	if len(e.positions) != 2 {
		t.Fatal("daily gate failed to reset")
	}
}
func TestRemainingDailyBudgetAndRewardGate(t *testing.T) {
	e := newTickEngine(TickConfig{Lots: .06, DailyLossLimit: 100, Balance: 1000, Contract: 100, MarginPerLot: 100, MaxGap: 60000})
	e.step(Tick{1000, 4100, 4100.2})
	e.r.Balance = 905
	e.pending = &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4099}
	e.step(Tick{2000, 4100, 4100.2})
	if e.r.DailyRejected != 1 {
		t.Fatal("remaining budget not reserved")
	}
	e.cfg.DailyLossLimit = 0
	e.cfg.MinRewardRisk = 1
	e.pending = &Signal{TP1: 4105, TP2: 4110, Side: "BUY", Stop: 4090}
	e.step(Tick{3000, 4100, 4100.2})
	if e.r.RewardRejected != 1 {
		t.Fatal("poor reward accepted")
	}
}
