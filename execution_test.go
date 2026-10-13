package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExecutionIntent(t *testing.T) {
	t.Setenv("BREAKOUT_LOOKBACK", "10")
	r := router("secret")
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8080"
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if w := call("GET", "/api/bridge/intent", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "/api/bridge/intent", "", "secret"); w.Code != 204 {
		t.Fatal(w.Code)
	}
	b := CandleBatch{Demo: true, Symbol: "XAUUSDm", Timeframe: "M3", CurrentBarTime: 2160}
	for i := 1; i <= 11; i++ {
		b.Candles = append(b.Candles, Candle{Time: int64(i * 180), Open: 4100, High: 4102, Low: 4098, Close: 4100})
	}
	b.Candles[8].Low = 4097
	b.Candles[10].Close = 4103
	b.Candles[10].High = 4104
	b = targetHistory(b)
	data, _ := json.Marshal(b)
	if w := call("POST", "/api/bridge/candles", string(data), "secret"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := call("GET", "/api/bridge/intent", "", "secret")
	if w.Code != 200 || w.Body.String() != "2880,BUY,4094.90000000,10,2.10000000,4108.00000000,4113.00000000" {
		t.Fatal(w.Code, w.Body.String())
	}
	b.Candles[15].Close = 4100
	data, _ = json.Marshal(b)
	call("POST", "/api/bridge/candles", string(data), "secret")
	if w = call("GET", "/api/bridge/intent", "", "secret"); w.Code != 204 {
		t.Fatal("old signal survived", w.Body.String())
	}
}

func TestExecutionTracking(t *testing.T) {
	t.Setenv("DEMO_LOGIN", "12345678")
	t.Setenv("DEMO_SERVER", "Example-Demo")
	r := router("secret")
	call := func(method, path string, body []byte, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		req.Host = "127.0.0.1:8080"
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	s := ExecutionSnapshot{Demo: true, Account: "12345678", Server: "Example-Demo", Version: "1.100", BrokerTime: 1000, Connected: true, Enabled: true, HistoryOK: true, Positions: []ExecutionPosition{}, Deals: []ExecutionDeal{{Ticket: "a", Identifier: "p", Time: 999, Leg: 1, Lots: .06, Price: 4100, Entry: 0, Net: -1}}, Events: []ExecutionEvent{{ID: "1", Time: 1000, Code: "signal_skipped", Detail: "Reward $40 below risk $60"}}}
	data, _ := json.Marshal(s)
	if w := call("POST", "/api/bridge/execution", data, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := call("POST", "/api/bridge/execution", data, "secret"); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	w := call("GET", "/api/execution", nil, "secret")
	var v executionView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.State != "ready" || len(v.Events) != 1 || len(v.Deals) != 1 {
		t.Fatal(v)
	}
	s.BrokerTime = 999
	data, _ = json.Marshal(s)
	if w = call("POST", "/api/bridge/execution", data, "secret"); w.Code != 409 {
		t.Fatal("old state accepted")
	}
	s.BrokerTime = 1001
	s.Demo = false
	data, _ = json.Marshal(s)
	if w = call("POST", "/api/bridge/execution", data, "secret"); w.Code != 400 {
		t.Fatal("real account accepted")
	}
}
func TestExecutionStates(t *testing.T) {
	now := time.Now()
	s := &ExecutionSnapshot{Connected: true, Enabled: true, HistoryOK: true}
	if executionState(s, now.Add(-16*time.Second), now) != "disconnected" {
		t.Fatal("stale marked ready")
	}
	s.Recovering = true
	s.Halted = true
	if executionState(s, now, now) != "recovering" {
		t.Fatal("recovery hidden")
	}
	s.Recovering = false
	if executionState(s, now, now) != "halted" {
		t.Fatal("halt hidden")
	}
	s.Halted = false
	s.Enabled = false
	if executionState(s, now, now) != "entries_disabled" {
		t.Fatal("disabled marked ready")
	}
}

func TestExecutionDashboardCookieIsReadOnly(t *testing.T) {
	r := router("secret")
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "127.0.0.1:8080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "dashboard" && c.Value != "" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no dashboard cookie")
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/execution", 200}, {"POST", "/api/bridge/execution", 401}, {"GET", "/api/bridge/intent", 401}} {
		req = httptest.NewRequest(tc.method, tc.path, nil)
		req.Host = "127.0.0.1:8080"
		req.AddCookie(cookie)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}
