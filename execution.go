package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"sync"
	"time"
)

type ExecutionPosition struct {
	Ticket     string  `json:"ticket"`
	Identifier string  `json:"identifier"`
	Leg        int     `json:"leg"`
	Side       string  `json:"side"`
	Lots       float64 `json:"lots"`
	Entry      float64 `json:"entry"`
	SL         float64 `json:"sl"`
	TP         float64 `json:"tp"`
	Profit     float64 `json:"profit"`
}
type ExecutionDeal struct {
	Ticket     string  `json:"ticket"`
	Identifier string  `json:"identifier"`
	Time       int64   `json:"time"`
	Leg        int     `json:"leg"`
	Entry      int     `json:"entry"`
	Lots       float64 `json:"lots"`
	Price      float64 `json:"price"`
	Net        float64 `json:"net"`
	Reason     string  `json:"reason"`
}
type ExecutionEvent struct {
	ID     string `json:"id"`
	Time   int64  `json:"time"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}
type ExecutionSnapshot struct {
	Symbol          string              `json:"symbol"`
	ControlRevision int64               `json:"control_revision"`
	ControlEnabled  bool                `json:"control_enabled"`
	ControlOK       bool                `json:"control_ok"`
	Demo            bool                `json:"demo"`
	Account         string              `json:"account"`
	Server          string              `json:"server"`
	Version         string              `json:"version"`
	BrokerTime      int64               `json:"broker_time"`
	Connected       bool                `json:"connected"`
	Enabled         bool                `json:"enabled"`
	Halted          bool                `json:"halted"`
	DailyBlocked    bool                `json:"daily_blocked"`
	Recovering      bool                `json:"recovering"`
	HistoryOK       bool                `json:"history_ok"`
	DailyRemaining  float64             `json:"daily_remaining"`
	DayEquityChange float64             `json:"day_equity_change"`
	Positions       []ExecutionPosition `json:"positions"`
	Deals           []ExecutionDeal     `json:"deals"`
	Events          []ExecutionEvent    `json:"events"`
}

func (s ExecutionSnapshot) validate(pin executionPin) error {
	if !s.Demo || pin.Account == "" || pin.Server == "" || s.Account != pin.Account || s.Server != pin.Server || s.BrokerTime <= 0 || len(s.Version) > 30 || len(s.Positions) > 100 || len(s.Deals) > 200 || len(s.Events) > 50 {
		return fmt.Errorf("invalid demo execution snapshot")
	}
	seen := map[string]bool{}
	for _, p := range s.Positions {
		if p.Ticket == "" || len(p.Ticket) > 30 || p.Identifier == "" || len(p.Identifier) > 30 || seen[p.Ticket] || (p.Leg != 1 && p.Leg != 2) || (p.Side != "BUY" && p.Side != "SELL") || p.Lots <= 0 || p.Entry <= 0 || p.SL < 0 || p.TP < 0 {
			return fmt.Errorf("invalid position")
		}
		seen[p.Ticket] = true
	}
	for _, d := range s.Deals {
		if d.Ticket == "" || len(d.Ticket) > 30 || d.Identifier == "" || len(d.Identifier) > 30 || d.Time <= 0 || d.Lots <= 0 || d.Price <= 0 || (d.Leg != 1 && d.Leg != 2) || d.Entry < 0 || d.Entry > 3 || len(d.Reason) > 100 {
			return fmt.Errorf("invalid deal")
		}
	}
	for _, e := range s.Events {
		if e.ID == "" || len(e.ID) > 80 || e.Time <= 0 || e.Code == "" || len(e.Code) > 80 || len(e.Detail) > 1000 {
			return fmt.Errorf("invalid event")
		}
	}
	return nil
}

type executionView struct {
	State      string             `json:"state"`
	ReceivedAt time.Time          `json:"received_at"`
	Snapshot   *ExecutionSnapshot `json:"snapshot"`
	Deals      []ExecutionDeal    `json:"deals"`
	Events     []ExecutionEvent   `json:"events"`
}

func executionState(s *ExecutionSnapshot, received, timeNow time.Time) string {
	if s == nil {
		return "waiting_for_executor"
	}
	if timeNow.Sub(received) > 15*time.Second || !s.Connected {
		return "disconnected"
	}
	if s.Recovering {
		return "recovering"
	}
	if s.Halted {
		return "halted"
	}
	if s.DailyBlocked {
		return "daily_limit"
	}
	if !s.Enabled {
		return "entries_disabled"
	}
	if (s.Version == "1.105" || s.Version == "1.106") || s.ControlRevision > 0 {
		if !s.ControlOK {
			return "control_unavailable"
		}
		if !s.ControlEnabled {
			return "dashboard_paused"
		}
	}
	if !s.HistoryOK {
		return "history_unavailable"
	}
	return "ready"
}
func executionRoutes(api *gin.RouterGroup, db *candleDatabase, symbols ...string) {
	symbol := selectedSymbol(symbols)
	pin, _ := loadExecutionPin() // Invalid/missing configuration rejects all execution uploads.
	var mu sync.Mutex
	v := executionView{Deals: []ExecutionDeal{}, Events: []ExecutionEvent{}}
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		var raw []byte
		err := db.pool.QueryRow(ctx, "SELECT payload,received_at FROM execution_snapshots WHERE account=$1 AND server=$2 AND symbol=$3", pin.Account, pin.Server, symbol).Scan(&raw, &v.ReceivedAt)
		if err == nil {
			_ = json.Unmarshal(raw, &v.Snapshot)
			v.ReceivedAt = time.Time{}
		}
		cancel()
	}
	api.POST("/bridge/execution", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 262144)
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		var s ExecutionSnapshot
		if err := dec.Decode(&s); err != nil {
			c.JSON(400, gin.H{"error": "invalid execution JSON"})
			return
		}
		if s.Symbol == "" && symbol == goldSymbol {
			s.Symbol = goldSymbol
		}
		if s.Symbol != symbol {
			c.AbortWithStatus(400)
			return
		}
		if dec.Decode(new(any)) != io.EOF || s.validate(pin) != nil {
			c.JSON(400, gin.H{"error": "invalid execution data"})
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if v.Snapshot != nil && s.BrokerTime < v.Snapshot.BrokerTime {
			c.JSON(409, gin.H{"error": "older execution snapshot"})
			return
		}
		now := time.Now()
		if db != nil {
			if err := db.saveExecution(c.Request.Context(), s, now); err != nil {
				c.JSON(503, gin.H{"error": "execution storage unavailable"})
				return
			}
		}
		v.Snapshot = &s
		v.ReceivedAt = now
		// Bounded in-memory read model is also used by tests without PostgreSQL.
		for _, d := range s.Deals {
			found := false
			for _, old := range v.Deals {
				if old.Ticket == d.Ticket {
					found = true
					break
				}
			}
			if !found {
				v.Deals = append(v.Deals, d)
			}
		}
		for _, e := range s.Events {
			found := false
			for _, old := range v.Events {
				if old.ID == e.ID {
					found = true
					break
				}
			}
			if !found {
				v.Events = append(v.Events, e)
			}
		}
		if len(v.Deals) > 200 {
			v.Deals = v.Deals[len(v.Deals)-200:]
		}
		if len(v.Events) > 50 {
			v.Events = v.Events[len(v.Events)-50:]
		}
		c.JSON(200, gin.H{"accepted": true})
	})
	api.GET("/execution", func(c *gin.Context) {
		mu.Lock()
		defer mu.Unlock()
		v.State = executionState(v.Snapshot, v.ReceivedAt, time.Now())
		if db != nil {
			if err := db.executionHistory(c.Request.Context(), &v, pin, symbol); err != nil {
				c.JSON(503, gin.H{"error": "execution history unavailable"})
				return
			}
		}
		c.JSON(200, v)
	})
}
func (db *candleDatabase) saveExecution(ctx context.Context, s ExecutionSnapshot, received time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if s.Symbol == "" {
		s.Symbol = goldSymbol
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution_snapshots(account,server,payload,received_at,symbol) VALUES($1,$2,$3,$4,$5) ON CONFLICT(account,server,symbol) DO UPDATE SET payload=EXCLUDED.payload,received_at=EXCLUDED.received_at`, s.Account, s.Server, raw, received, s.Symbol)
	if err != nil {
		return err
	}
	for _, d := range s.Deals {
		raw, _ = json.Marshal(d)
		_, err = tx.Exec(ctx, `INSERT INTO execution_deals(account,server,ticket,broker_time,payload,symbol) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, s.Account, s.Server, d.Ticket, d.Time, raw, s.Symbol)
		if err != nil {
			return err
		}
	}
	for _, e := range s.Events {
		raw, _ = json.Marshal(e)
		_, err = tx.Exec(ctx, `INSERT INTO execution_events(account,server,id,broker_time,payload,symbol) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, s.Account, s.Server, e.ID, e.Time, raw, s.Symbol)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (db *candleDatabase) executionHistory(ctx context.Context, v *executionView, pin executionPin, symbols ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := db.pool.Query(ctx, `SELECT payload FROM execution_deals WHERE account=$1 AND server=$2 AND symbol=$3 ORDER BY broker_time DESC,ticket DESC LIMIT 200`, pin.Account, pin.Server, selectedSymbol(symbols))
	if err != nil {
		return err
	}
	defer rows.Close()
	deals := []ExecutionDeal{}
	for rows.Next() {
		var raw []byte
		var d ExecutionDeal
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &d); err != nil {
			return err
		}
		deals = append(deals, d)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	rows, err = db.pool.Query(ctx, `SELECT payload FROM execution_events WHERE account=$1 AND server=$2 AND symbol=$3 ORDER BY broker_time DESC,id DESC LIMIT 50`, pin.Account, pin.Server, selectedSymbol(symbols))
	if err != nil {
		return err
	}
	defer rows.Close()
	events := []ExecutionEvent{}
	for rows.Next() {
		var raw []byte
		var e ExecutionEvent
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return err
		}
		events = append(events, e)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	v.Deals = deals
	v.Events = events
	return nil
}
