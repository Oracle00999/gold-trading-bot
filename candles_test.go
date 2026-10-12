package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCandleValidation(t *testing.T) {
	valid := func() CandleBatch {
		return CandleBatch{Demo: true, Symbol: "XAUUSDm", Timeframe: "M5", CurrentBarTime: 900, Candles: []Candle{{Time: 300, Open: 10, High: 12, Low: 9, Close: 11, TickVolume: 3}, {Time: 600, Open: 11, High: 12, Low: 8, Close: 9}}}
	}
	if err := valid().validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*CandleBatch){func(b *CandleBatch) { b.Demo = false }, func(b *CandleBatch) { b.Candles[1].Time = 900 }, func(b *CandleBatch) { b.Candles[1].Time = 300 }, func(b *CandleBatch) { b.Candles[0].High = 9 }, func(b *CandleBatch) { b.Candles[0].TickVolume = -1 }, func(b *CandleBatch) { b.Timeframe = "M1" }} {
		b := valid()
		change(&b)
		if b.validate() == nil {
			t.Fatal("accepted invalid batch")
		}
	}
}
func TestCandleRoutes(t *testing.T) {
	r := router("test")
	body := `{"demo":true,"symbol":"XAUUSDm","timeframe":"M5","current_bar_time":900,"candles":[{"time":600,"open":10,"high":12,"low":9,"close":11,"tick_volume":3}]}`
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8080"
		req.Header.Set("Authorization", "Bearer test")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for i := 0; i < 2; i++ {
		w := request("POST", "/api/bridge/candles", body)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	w := request("GET", "/api/candles?timeframe=M5", "")
	if w.Code != 200 || strings.Count(w.Body.String(), `"tick_volume"`) != 1 {
		t.Fatal(w.Body.String())
	}
	old := strings.ReplaceAll(strings.ReplaceAll(body, "900", "600"), `"time":600`, `"time":300`)
	if request("POST", "/api/bridge/candles", old).Code != 409 {
		t.Fatal("accepted older batch")
	}
	if request("POST", "/api/bridge/candles", body+"{}").Code != 400 {
		t.Fatal("accepted extra object")
	}
}

func TestFormingCandleSeparate(t *testing.T) {
	b := CandleBatch{Demo: true, Symbol: "XAUUSDm", Timeframe: "M5", CurrentBarTime: 900, Candles: []Candle{{Time: 600, Open: 10, High: 12, Low: 9, Close: 11}}, Forming: &Candle{Time: 900, Open: 11, High: 13, Low: 10, Close: 12}}
	if err := b.validate(); err != nil {
		t.Fatal(err)
	}
	b.Forming.Time = 600
	if b.validate() == nil {
		t.Fatal("accepted forming candle at wrong time")
	}
	b.Forming.Time = 900
	b.Forming.High = 10
	if b.validate() == nil {
		t.Fatal("accepted invalid forming high")
	}
}
