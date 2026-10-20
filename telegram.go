package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// executionNotifier receives new, persisted MT5 executor events after the
// bridge request has completed its database transaction.
type executionNotifier interface {
	Notify(ExecutionSnapshot, []ExecutionEvent)
}

type telegramMessage struct {
	Symbol string
	Event  ExecutionEvent
	Text   string
}

type telegramNotifier struct {
	token   string
	chatID  string
	apiBase string
	client  *http.Client
	jobs    chan telegramMessage
	done    chan struct{}
	wg      sync.WaitGroup
}

func loadTelegramNotifier(path string) (*telegramNotifier, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid Telegram configuration line")
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	token, chatID := values["TELEGRAM_BOT_TOKEN"], values["TELEGRAM_CHAT_ID"]
	if token == "" || chatID == "" || strings.ContainsAny(token+chatID, " \t\r\n") || len(token) > 256 || len(chatID) > 128 {
		return nil, fmt.Errorf("telegram.env must contain TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID")
	}
	return newTelegramNotifier(token, chatID, "https://api.telegram.org", &http.Client{Timeout: 8 * time.Second}), nil
}

func newTelegramNotifier(token, chatID, apiBase string, client *http.Client) *telegramNotifier {
	n := &telegramNotifier{token: token, chatID: chatID, apiBase: strings.TrimRight(apiBase, "/"), client: client, jobs: make(chan telegramMessage, 64), done: make(chan struct{})}
	n.wg.Add(1)
	go n.run()
	return n
}

func (n *telegramNotifier) Close() {
	close(n.done)
	n.wg.Wait()
}

func telegramEvent(code string) bool {
	switch code {
	case "pair_opened", "early_profit_lock", "daily_limit", "halted", "recovery_close", "recovery_reset":
		return true
	default:
		return false
	}
}

func telegramText(snapshot ExecutionSnapshot, event ExecutionEvent) string {
	title := strings.ReplaceAll(event.Code, "_", " ")
	title = strings.ToUpper(title[:1]) + title[1:]
	lines := []string{"Gold Bot · DEMO", snapshot.Symbol + " — " + title, event.Detail}
	if event.Code == "pair_opened" {
		for _, p := range snapshot.Positions {
			lines = append(lines, fmt.Sprintf("Leg %d: %s %.2f @ %.5f | SL %.5f | TP %.5f", p.Leg, p.Side, p.Lots, p.Entry, p.SL, p.TP))
		}
	}
	return strings.Join(lines, "\n")
}

func (n *telegramNotifier) Notify(snapshot ExecutionSnapshot, events []ExecutionEvent) {
	for _, event := range events {
		if !telegramEvent(event.Code) {
			continue
		}
		job := telegramMessage{Symbol: snapshot.Symbol, Event: event, Text: telegramText(snapshot, event)}
		select {
		case n.jobs <- job:
		default:
			log.Printf("Telegram notification queue full; dropping %s for %s", event.Code, snapshot.Symbol)
		}
	}
}

func (n *telegramNotifier) run() {
	defer n.wg.Done()
	for {
		select {
		case <-n.done:
			return
		case job := <-n.jobs:
			if err := n.send(job); err != nil {
				log.Printf("Telegram notification failed for %s/%s: %v", job.Symbol, job.Event.Code, err)
			}
		}
	}
}

func (n *telegramNotifier) send(job telegramMessage) error {
	payload, _ := json.Marshal(struct {
		ChatID                string `json:"chat_id"`
		Text                  string `json:"text"`
		DisableWebPagePreview bool   `json:"disable_web_page_preview"`
	}{n.chatID, job.Text, true})
	var lastErr error
	for attempt, delay := range []time.Duration{0, time.Second, 3 * time.Second} {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-n.done:
				timer.Stop()
				return context.Canceled
			case <-timer.C:
			}
		}
		req, err := http.NewRequest(http.MethodPost, n.apiBase+"/bot"+n.token+"/sendMessage", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := n.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 65536))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		var result struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
		}
		if json.Unmarshal(body, &result) == nil && response.StatusCode/100 == 2 && result.OK {
			return nil
		}
		lastErr = fmt.Errorf("telegram HTTP %d: %s", response.StatusCode, result.Description)
	}
	return lastErr
}
