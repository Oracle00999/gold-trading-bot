package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelegramNotifierSendsPairOpened(t *testing.T) {
	received := make(chan struct {
		Path string
		Text string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		received <- struct{ Path, Text string }{r.URL.Path, body.Text}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	n := newTelegramNotifier("secret", "123", server.URL, server.Client())
	defer n.Close()
	n.Notify(ExecutionSnapshot{Symbol: goldSymbol, Positions: []ExecutionPosition{{Leg: 1, Side: "BUY", Lots: .06, Entry: 4100, SL: 4090, TP: 4110}}}, []ExecutionEvent{{Code: "pair_opened", Detail: "Two protected positions"}})
	select {
	case got := <-received:
		if got.Path != "/botsecret/sendMessage" || !strings.Contains(got.Text, "XAUUSDm — Pair opened") || !strings.Contains(got.Text, "Leg 1: BUY") {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("Telegram message was not sent")
	}
}

func TestTelegramEventFilter(t *testing.T) {
	if !telegramEvent("pair_opened") || !telegramEvent("early_profit_lock") || telegramEvent("signal_skipped") {
		t.Fatal("unexpected Telegram event filter")
	}
}
