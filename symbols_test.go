package main

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEuroBreakoutUsesPips(t *testing.T) {
	b := CandleBatch{Demo: true, Symbol: goldSymbol, Timeframe: "M3", CurrentBarTime: 2160}
	for i := 1; i <= 11; i++ {
		b.Candles = append(b.Candles, Candle{Time: int64(i * 180), Open: 4100, High: 4102, Low: 4098, Close: 4100})
	}
	b.Candles[8].Low = 4097
	b.Candles[10].Close = 4103
	b.Candles[10].High = 4104
	b = targetHistory(b)
	b.Symbol = euroSymbol
	for i := range b.Candles {
		c := &b.Candles[i]
		c.Open = 1 + c.Open/10000
		c.High = 1 + c.High/10000
		c.Low = 1 + c.Low/10000
		c.Close = 1 + c.Close/10000
	}
	s, state := breakout(b, StrategyConfig{10, .0002})
	if s == nil || s.Side != "BUY" || math.Abs(s.TP1-1.4108) > 1e-10 || math.Abs(s.TP2-1.4113) > 1e-10 || math.Abs(s.Stop-1.4095) > 1e-10 {
		t.Fatal(state, s)
	}
	cfg, err := strategyConfig(euroSymbol)
	if err != nil || cfg.StopBuffer != .0002 {
		t.Fatal(cfg, err)
	}
}
func TestSymbolRoutesAreIsolated(t *testing.T) {
	t.Setenv("DEMO_LOGIN", "123")
	t.Setenv("DEMO_SERVER", "Demo")
	r := router("secret")
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "localhost:8080"
		req.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	page := call("GET", "/", "")
	cookies := page.Result().Cookies()
	for _, method := range []string{"GET", "POST"} {
		path := "/api/pairs/EURUSDm/status"
		want := 200
		if method == "POST" {
			path = "/api/pairs/EURUSDm/bridge/snapshot"
			want = 401
		}
		req := httptest.NewRequest(method, path, nil)
		req.Host = "localhost:8080"
		req.AddCookie(cookies[len(cookies)-1])
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("EUR cookie permissions %s: %d", method, w.Code)
		}
	}
	euro := "/api/pairs/EURUSDm"
	body := `{"demo":true,"connected":true,"currency":"USD","balance":1000,"equity":1000,"symbol":"EURUSDm","bid":1.15,"ask":1.1501,"tick_time_ms":1}`
	if w := call("POST", euro+"/bridge/snapshot", body); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("POST", "/api/bridge/snapshot", body); w.Code != 400 {
		t.Fatal("cross-symbol snapshot accepted")
	}
	if w := call("GET", "/api/status", ""); !strings.Contains(w.Body.String(), "waiting_for_mt5") {
		t.Fatal("gold overwritten")
	}
	batch := CandleBatch{Demo: true, Symbol: euroSymbol, Timeframe: "M3", CurrentBarTime: 360, Candles: []Candle{{Time: 180, Open: 1.1, Close: 1.1, High: 1.2, Low: 1}}}
	raw, _ := json.Marshal(batch)
	if w := call("POST", euro+"/bridge/candles", string(raw)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("POST", "/api/bridge/candles", string(raw)); w.Code != 400 {
		t.Fatal("cross-symbol candles accepted")
	}
	s := ExecutionSnapshot{Symbol: euroSymbol, Demo: true, Account: "123", Server: "Demo", Version: "1.106", BrokerTime: 123, Connected: true}
	raw, _ = json.Marshal(s)
	if w := call("POST", euro+"/bridge/execution", string(raw)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("POST", "/api/bridge/execution", string(raw)); w.Code != 400 {
		t.Fatal("cross-symbol execution accepted")
	}
	if w := call("GET", "/api/execution", ""); !strings.Contains(w.Body.String(), "waiting_for_executor") {
		t.Fatal("gold execution overwritten")
	}
	if w := call("GET", euro+"/execution", ""); !strings.Contains(w.Body.String(), "EURUSDm") {
		t.Fatal("EUR execution missing")
	}
}
