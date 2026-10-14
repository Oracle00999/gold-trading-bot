package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBridge(t *testing.T) {
	r := router("test-token")
	valid := `{"demo":true,"connected":true,"currency":"USD","balance":1000,"equity":1000,"symbol":"XAUUSDm","bid":2600,"ask":2600.5,"tick_time_ms":123456789}`
	cases := []struct {
		name, token, body string
		status            int
	}{
		{"unauthenticated", "", valid, 401},
		{"real account", "test-token", strings.Replace(valid, `"demo":true`, `"demo":false`, 1), 403},
		{"wrong symbol", "test-token", strings.Replace(valid, "XAUUSDm", "EURUSD", 1), 400},
		{"crossed quote", "test-token", strings.Replace(valid, "2600.5", "2599", 1), 400},
		{"trailing JSON", "test-token", valid + `{}`, 400},
		{"oversized", "test-token", strings.Repeat(" ", 4097) + valid, 400},
		{"valid demo", "test-token", valid, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/bridge/snapshot", strings.NewReader(tc.body))
			req.Host = "127.0.0.1:8080"
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"connected"`) || !strings.Contains(w.Body.String(), `"balance":1000`) {
		t.Fatal(w.Body.String())
	}
}

func TestWaiting(t *testing.T) {
	r := router("test-token")
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "waiting_for_mt5") {
		t.Fatal(w.Body.String())
	}
}

func TestDashboardReadOnlySession(t *testing.T) {
	r := router("secret")
	request := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "127.0.0.1:8080"
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	page := request("GET", "/", nil)
	if page.Code != 200 || strings.Contains(page.Body.String(), "secret") {
		t.Fatal("invalid dashboard")
	}
	cookies := page.Result().Cookies()
	if len(cookies) != 2 || cookies[0].Path != "/api/status" || cookies[0].MaxAge != -1 {
		t.Fatal("missing session")
	}
	if request("GET", "/api/status", cookies[1]).Code != 200 {
		t.Fatal("session cannot read")
	}
	if request("POST", "/api/bridge/snapshot", cookies[1]).Code != 401 {
		t.Fatal("session can write")
	}
	if request("GET", "/api/status", nil).Code != 401 {
		t.Fatal("unauthenticated read")
	}
	req := httptest.NewRequest("GET", "http://attacker.invalid/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("unexpected host allowed")
	}
}

func TestPrivateDashboardHost(t *testing.T) {
	for _, host := range []string{"localhost:8080", "127.0.0.1:8080", "bot.example.ts.net", "bot.example.ts.net:443"} {
		if !dashboardHostAllowed(host, "bot.example.ts.net") {
			t.Fatal(host)
		}
	}
	for _, host := range []string{"evil.example", "bot.example.ts.net.evil", "bot.example.ts.net:8080", ""} {
		if dashboardHostAllowed(host, "bot.example.ts.net") {
			t.Fatal(host)
		}
	}
	if dashboardHostAllowed("bot.example.ts.net", "") {
		t.Fatal("private access enabled without configuration")
	}
}
