package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestPostgresPersistence(t *testing.T) {
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("set TEST_POSTGRES=1 for local database integration")
	}
	db, err := openDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer db.pool.Close()
	// Isolate test rows from the real XAUUSDm feed.
	symbol := "test-" + time.Now().Format("150405.000000000")
	defer db.pool.Exec(context.Background(), "DELETE FROM candles WHERE symbol=$1", symbol)
	defer db.pool.Exec(context.Background(), "DELETE FROM candle_windows WHERE symbol=$1", symbol)
	b := CandleBatch{Demo: true, Symbol: symbol, Timeframe: "M5", CurrentBarTime: 900, Candles: []Candle{{Time: 600, Open: 10, High: 12, Low: 9, Close: 11}}, Forming: &Candle{Time: 900, Open: 11, High: 12, Low: 10, Close: 12}}
	for i := 0; i < 2; i++ {
		if err = db.save(context.Background(), b, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.pool.QueryRow(context.Background(), "SELECT count(*) FROM candles WHERE symbol=$1", symbol).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate/forming persisted: %d %v", count, err)
	}
	b.Candles[0].Close = 12
	if err = db.save(context.Background(), b, time.Now()); err != nil {
		t.Fatal(err)
	}
	other, err := openDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer other.pool.Close()
	var close float64
	if err = other.pool.QueryRow(context.Background(), "SELECT close FROM candles WHERE symbol=$1", symbol).Scan(&close); err != nil || close != 12 {
		t.Fatalf("reopen/correction failed: %v %v", close, err)
	}
}

func TestPostgresReloadHistory(t *testing.T) {
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("local database integration")
	}
	db, err := openDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer db.pool.Close()
	windows, err := db.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) == 0 {
		t.Skip("no live history saved yet")
	}
	for tf, w := range windows {
		if !w.Restored || w.Forming != nil || len(w.Candles) == 0 {
			t.Fatalf("invalid restored %s window", tf)
		}
		if err := w.validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutionPersistence(t *testing.T) {
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("local PostgreSQL integration")
	}
	db, err := openDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer db.pool.Close()
	account := "test-" + time.Now().Format("150405.000000000")
	ctx := context.Background()
	defer db.pool.Exec(ctx, "DELETE FROM execution_snapshots WHERE account=$1", account)
	defer db.pool.Exec(ctx, "DELETE FROM execution_deals WHERE account=$1", account)
	defer db.pool.Exec(ctx, "DELETE FROM execution_events WHERE account=$1", account)
	s := ExecutionSnapshot{Account: account, Server: "test", BrokerTime: 1000, Deals: []ExecutionDeal{{Ticket: "1", Time: 999, Net: 12}}, Events: []ExecutionEvent{{ID: "1", Time: 999, Code: "test"}}}
	for i := 0; i < 2; i++ {
		if err = db.saveExecution(ctx, s, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"execution_snapshots", "execution_deals", "execution_events"} {
		var count int
		if err = db.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE account=$1", account).Scan(&count); err != nil || count != 1 {
			t.Fatal(table, count, err)
		}
	}
	other, err := openDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer other.pool.Close()
	var view executionView
	if err = other.executionHistory(ctx, &view, executionPin{Account: account, Server: "test"}); err != nil || len(view.Deals) != 1 || len(view.Events) != 1 {
		t.Fatal("configured account history not restored", err)
	}
	if err = other.executionHistory(ctx, &view, executionPin{Account: account, Server: "other"}); err != nil || len(view.Deals) != 0 || len(view.Events) != 0 {
		t.Fatal("history crossed server boundary", err)
	}
	var raw []byte
	if err = other.pool.QueryRow(ctx, "SELECT payload FROM execution_snapshots WHERE account=$1", account).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var restored ExecutionSnapshot
	if err = json.Unmarshal(raw, &restored); err != nil || len(restored.Deals) != 1 || restored.Deals[0].Net != 12 {
		t.Fatal("restart lost execution state", err)
	}
}
