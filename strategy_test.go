package main

import (
	"math"
	"testing"
)

func TestBreakout(t *testing.T) {
	makeBatch := func() CandleBatch {
		b := CandleBatch{Demo: true, Symbol: "XAUUSDm", Timeframe: "M3", CurrentBarTime: 2160}
		for i := 1; i <= 11; i++ {
			b.Candles = append(b.Candles, Candle{Time: int64(i * 180), Open: 4100, High: 4102, Low: 4098, Close: 4100})
		}
		b.Candles[8].Low = 4097
		b.Candles[8].High = 4102.5
		return b
	}
	cfg := StrategyConfig{10, .1}
	b := makeBatch()
	b.Candles[10].Close = 4103
	b.Candles[10].High = 4104
	s, _ := breakout(targetHistory(b), cfg)
	if s == nil || s.Side != "BUY" || s.Resistance != 4102.5 || s.TP1 != 4108 || s.TP2 != 4113 || s.Stop != 4096.9 {
		t.Fatalf("bad buy: %+v", s)
	}
	b.Candles[10].Close = 4096
	b.Candles[10].Low = 4095
	s, _ = breakout(targetHistory(b), cfg)
	if s == nil || s.Side != "SELL" || s.TP1 != 4091 || s.Stop != 4102.6 {
		t.Fatalf("bad sell: %+v", s)
	}
	b = makeBatch()
	b.Candles[10].High = 4120
	b.Forming = &Candle{Close: 4200}
	if s, _ := breakout(targetHistory(b), cfg); s != nil {
		t.Fatal("wick or forming candle triggered")
	}
	b.Candles[10].Close = 4102
	if s, _ := breakout(targetHistory(b), cfg); s != nil {
		t.Fatal("equality triggered")
	}
	b.Candles[10].Close = 4103
	b.Candles[5].Time -= 180
	if s, _ := breakout(targetHistory(b), cfg); s != nil {
		t.Fatal("gap accepted")
	}
	b = makeBatch()
	if s, _ := breakout(targetHistory(b), StrategyConfig{20, .1}); s != nil {
		t.Fatal("insufficient history accepted")
	}
}

func TestNearestSwing(t *testing.T) {
	bars := []Candle{{Low: 10, High: 20}, {Low: 5, High: 25}, {Low: 11, High: 19}, {Low: 9, High: 21}, {Low: 12, High: 18}}
	if nearestSwing(bars, true) != 9 || nearestSwing(bars, false) != 21 {
		t.Fatal("selected extreme instead of most recent pivot")
	}
	bars[4].Low = 8
	if nearestSwing(bars, true) != 5 {
		t.Fatal("unconfirmed last candle used")
	}
	if nearestSwing([]Candle{{Low: 1}, {Low: 1}, {Low: 2}}, true) != 0 {
		t.Fatal("flat lows accepted")
	}
}

func targetHistory(b CandleBatch) CandleBatch {
	b.Candles = append([]Candle(nil), b.Candles...)
	for i := range b.Candles {
		b.Candles[i].Time += 900
	}
	b.CurrentBarTime += 900
	old := []Candle{}
	for i := 0; i < 5; i++ {
		old = append(old, Candle{Time: int64((i + 1) * 180), Open: 4100, Close: 4100, High: 4102, Low: 4098})
	}
	old[1].High = 4113
	old[1].Low = 4086
	old[3].High = 4108
	old[3].Low = 4091
	b.Candles = append(old, b.Candles...)
	return b
}

func TestStructuralTargets(t *testing.T) {
	bars := []Candle{}
	for i := 0; i < 9; i++ {
		bars = append(bars, Candle{Time: int64(i * 180), High: 100, Low: 90})
	}
	bars[1].High = 115
	bars[3].High = 108
	bars[5].High = 108
	bars[7].High = 112
	bars[1].Low = 75
	bars[3].Low = 88
	bars[5].Low = 88
	bars[7].Low = 82
	a, b, ok := structuralTargets(bars, 101, true)
	if !ok || a != 108 || b != 112 {
		t.Fatal(a, b, ok)
	}
	a, b, ok = structuralTargets(bars, 89, false)
	if !ok || a != 82 || b != 75 {
		t.Fatal(a, b, ok)
	}
	// The final candle cannot confirm itself as a pivot.
	bars[8].High = 120
	if _, _, ok = structuralTargets(bars, 116, true); ok {
		t.Fatal("unconfirmed extreme used")
	}
	// A pivot spanning missing candles is invalid.
	bars[3].Time++
	bars[5].Time++
	a, _, _ = structuralTargets(bars, 101, true)
	if a == 108 {
		t.Fatal("gap pivot used")
	}
}
func TestStructuralTargetsRequireEligibleLevel(t *testing.T) {
	b := targetHistory(CandleBatch{Candles: []Candle{}})
	if _, _, ok := structuralTargets(b.Candles, 4110, true); ok {
		t.Fatal("one target accepted")
	}
}

func TestTargetMinimumDistance(t *testing.T) {
	for _, buy := range []bool{true, false} {
		bars := make([]Candle, 9)
		for i := range bars {
			bars[i] = Candle{Time: int64(i * 180), High: 4100, Low: 4100}
		}
		for i, distance := range []float64{0.233, 4.999, 5, 7} {
			if buy {
				bars[2*i+1].High = 4100 + distance
			} else {
				bars[2*i+1].Low = 4100 - distance
			}
		}
		a, b, ok := structuralTargets(bars, 4100, buy)
		d := 1.0
		if !buy {
			d = -1
		}
		if !ok || a != 4100+d*5 || b != 4100+d*7 {
			t.Fatal(buy, a, b, ok)
		}
		a, b, ok = structuralTargets(bars, 4100+d*0.1, buy)
		if !ok || a != 4100+d*7 || b != a {
			t.Fatal("single level not reused", a, b, ok)
		}
		if _, _, ok = structuralTargets(bars, 4100+d*3, buy); ok {
			t.Fatal("accepted no eligible target")
		}
	}
}

func TestWiderDefaultStop(t *testing.T) {
	t.Setenv("BREAKOUT_STOP_BUFFER", "")
	t.Setenv("BREAKOUT_LOOKBACK", "10")
	cfg, err := strategyConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, buy := range []bool{true, false} {
		b := CandleBatch{Demo: true, Symbol: "XAUUSDm", Timeframe: "M3", CurrentBarTime: 2160}
		for i := 1; i <= 11; i++ {
			b.Candles = append(b.Candles, Candle{Time: int64(i * 180), Open: 4100, High: 4102, Low: 4098, Close: 4100})
		}
		b.Candles[8].Low = 4097
		b.Candles[8].High = 4102.5
		if buy {
			b.Candles[10].Close = 4103
			b.Candles[10].High = 4104
		} else {
			b.Candles[10].Close = 4096
			b.Candles[10].Low = 4095
		}
		old, _ := breakout(targetHistory(b), StrategyConfig{10, .1})
		widened, _ := breakout(targetHistory(b), cfg)
		if old == nil || widened == nil {
			t.Fatal("expected signal")
		}
		delta := widened.Stop - old.Stop
		want := 2.0
		if buy {
			want = -2
		}
		if math.Abs(delta-want) > 1e-8 {
			t.Fatalf("buy=%v stop change %v, want %v", buy, delta, want)
		}
		if widened.TP1 != old.TP1 || widened.TP2 != old.TP2 {
			t.Fatal("targets changed")
		}
	}
}
