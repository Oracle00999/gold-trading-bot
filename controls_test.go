package main

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestControlAuthentication(t *testing.T) {
	a := newControlAuth("gold.example.ts.net", "owner@example.com")
	r := gin.New()
	r.GET("/", func(c *gin.Context) { a.issue(c); c.Status(200) })
	controlRoutes(r, r.Group("/api"), nil, a)
	request := func(handler http.Handler, method, path, identity, origin, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://gold.example.ts.net"+path, strings.NewReader(`{"enabled":false,"revision":1}`))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Tailscale-User-Login", identity)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Authorization", "Bearer bridge-secret")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	proxy := privateDashboardHandler(r)
	page := request(proxy, "GET", "/", a.owner, "", "", nil)
	cookies := page.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("insecure owner session")
	}
	cookie := cookies[0]
	cases := []struct {
		name                   string
		h                      http.Handler
		identity, origin, csrf string
		cookie                 *http.Cookie
		want                   int
	}{
		{"direct forged owner", r, a.owner, "https://" + a.host, a.csrf, cookie, 403},
		{"other tailnet user", proxy, "other@example.com", "https://" + a.host, a.csrf, cookie, 403},
		{"no identity", proxy, "", "https://" + a.host, a.csrf, cookie, 403},
		{"bridge token no session", proxy, a.owner, "https://" + a.host, a.csrf, nil, 403},
		{"cross site", proxy, a.owner, "https://evil.example", a.csrf, cookie, 403},
		{"missing csrf", proxy, a.owner, "https://" + a.host, "", cookie, 403},
		{"valid auth database down", proxy, a.owner, "https://" + a.host, a.csrf, cookie, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := request(tc.h, "POST", "/api/controls/XAUUSDm", tc.identity, tc.origin, tc.csrf, tc.cookie)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
	if w := request(proxy, "GET", "/api/bridge/control", a.owner, "", "", cookie); w.Code != 403 {
		t.Fatal("private ingress exposed machine API")
	}
	expiry := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	expired := *cookie
	expired.Value = expiry + "." + a.sign(expiry)
	if w := request(proxy, "GET", "/api/controls/XAUUSDm", a.owner, "", "", &expired); w.Code != 403 {
		t.Fatal("expired session accepted")
	}
	if w := request(r, "GET", "/", a.owner, "", "", nil); len(w.Result().Cookies()) != 0 {
		t.Fatal("direct ingress issued control session")
	}
	req := httptest.NewRequest("GET", "https://gold.example.ts.net/", nil)
	req.RemoteAddr = "100.64.0.1:4321"
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("non-loopback proxy accepted")
	}
}
func TestControlMissingDatabaseFailsClosed(t *testing.T) {
	var db *candleDatabase
	if _, err := db.tradingControl(context.Background()); err == nil {
		t.Fatal("control must fail closed")
	}
	if err := db.changeControl(context.Background(), true, 1, "owner"); err == nil {
		t.Fatal("must not claim successful save")
	}
}

// Use an isolated schema: never flip the real account's trading permission.
func TestControlPostgres(t *testing.T) {
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("local PostgreSQL integration")
	}
	base, err := openDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer base.pool.Close()
	ctx := context.Background()
	schema := "test_controls_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = base.pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer base.pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg := base.pool.Config()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &candleDatabase{pool: pool}
	if _, err = pool.Exec(ctx, executionMigration); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, controlMigration); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, euroMigration); err != nil {
		t.Fatal(err)
	}
	initial, err := db.tradingControl(ctx)
	if err != nil || initial.Enabled || initial.Revision != 1 {
		t.Fatal(initial, err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- db.changeControl(ctx, true, 1, "owner@example.com") }()
	}
	good, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			good++
		} else if errors.Is(err, errControlConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if good != 1 || conflicts != 1 {
		t.Fatal("concurrent update was not serialized")
	}
	reopened, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	other := &candleDatabase{pool: reopened}
	saved, err := other.tradingControl(ctx)
	if err != nil || !saved.Enabled || saved.Revision != 2 || saved.ChangedBy != "owner@example.com" {
		t.Fatal("permission did not persist", saved, err)
	}
	if err = other.changeControl(ctx, false, saved.Revision, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	pin := executionPin{Account: "test", Server: "demo"}
	saved, _ = db.tradingControl(ctx)
	ack, err := db.controlAcknowledged(ctx, pin, saved)
	if err != nil || ack {
		t.Fatal("missing executor acknowledged", err)
	}
	payload := `{"control_revision":3,"control_enabled":false,"control_ok":true,"connected":true}`
	if _, err = pool.Exec(ctx, "INSERT INTO execution_snapshots VALUES($1,$2,$3,now())", pin.Account, pin.Server, payload); err != nil {
		t.Fatal(err)
	}
	ack, err = db.controlAcknowledged(ctx, pin, saved)
	if err != nil || !ack {
		t.Fatal("fresh acknowledgement missing", err)
	}
	saved.Revision++
	ack, err = db.controlAcknowledged(ctx, pin, saved)
	if err != nil || ack {
		t.Fatal("old revision acknowledged", err)
	}
	saved.Revision--
	pool.Exec(ctx, "UPDATE execution_snapshots SET received_at=now()-interval '30 seconds'")
	ack, err = db.controlAcknowledged(ctx, pin, saved)
	if err != nil || ack {
		t.Fatal("stale executor acknowledged", err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM trading_control_audit").Scan(&count); err != nil || count != 2 {
		t.Fatal("missing audit", count, err)
	}
	euro, err := other.tradingControl(ctx, euroSymbol)
	if err != nil || euro.Enabled || euro.Revision != 1 {
		t.Fatal("EUR defaults not isolated", euro, err)
	}
	if err = other.changeControl(ctx, true, 1, "owner@example.com", euroSymbol); err != nil {
		t.Fatal(err)
	}
	// Identical event IDs across symbols must not erase either event.
	for _, symbol := range []string{goldSymbol, euroSymbol} {
		snapshot := ExecutionSnapshot{Symbol: symbol, Account: "isolation", Server: "demo", BrokerTime: 1, Events: []ExecutionEvent{{ID: "shared", Time: 1, Code: symbol}}}
		if err = other.saveExecution(ctx, snapshot, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, symbol := range []string{goldSymbol, euroSymbol} {
		var view executionView
		if err = other.executionHistory(ctx, &view, executionPin{Account: "isolation", Server: "demo"}, symbol); err != nil || len(view.Events) != 1 || view.Events[0].Code != symbol {
			t.Fatal("history crossed symbols", view, err)
		}
	}
	var snapshots int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM execution_snapshots WHERE account='isolation'").Scan(&snapshots); err != nil || snapshots != 2 {
		t.Fatal("snapshot overwritten", snapshots, err)
	}
	saved, err = other.tradingControl(ctx)
	if err != nil || saved.Enabled || saved.Revision != 3 {
		t.Fatal(saved, err)
	}
}

func TestResumeRequiresFreshSignal(t *testing.T) {
	now := time.Now()
	s := TradingControl{Enabled: true, ChangedAt: now}
	if controlAllowsSignal(s, now.Add(-time.Second)) || controlAllowsSignal(s, now) || controlAllowsSignal(s, time.Time{}) {
		t.Fatal("resumed old signal")
	}
	if !controlAllowsSignal(s, now.Add(time.Second)) {
		t.Fatal("fresh signal rejected")
	}
	s.Enabled = false
	if controlAllowsSignal(s, now.Add(time.Second)) {
		t.Fatal("disabled pair allowed")
	}
}

func TestExecutionShowsDashboardPermission(t *testing.T) {
	now := time.Now()
	s := &ExecutionSnapshot{Version: "1.105", Connected: true, Enabled: true, HistoryOK: true}
	if got := executionState(s, now, now); got != "control_unavailable" {
		t.Fatal(got)
	}
	s.ControlOK = true
	s.ControlRevision = 1
	if got := executionState(s, now, now); got != "dashboard_paused" {
		t.Fatal(got)
	}
	s.ControlEnabled = true
	if got := executionState(s, now, now); got != "ready" {
		t.Fatal(got)
	}
}
