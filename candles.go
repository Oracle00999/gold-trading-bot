package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"sync"
	"time"
)

type Candle struct {
	Time       int64   `json:"time"`
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Close      float64 `json:"close"`
	TickVolume int64   `json:"tick_volume"`
}
type CandleBatch struct {
	Demo           bool     `json:"demo"`
	Symbol         string   `json:"symbol"`
	Timeframe      string   `json:"timeframe"`
	CurrentBarTime int64    `json:"current_bar_time"`
	Candles        []Candle `json:"candles"`
	Forming        *Candle  `json:"forming,omitempty"`
}

func (b CandleBatch) validate() error {
	if !b.Demo || !supportedSymbol(b.Symbol) {
		return fmt.Errorf("supported demo symbols only")
	}
	seconds := map[string]int64{"M3": 180, "M5": 300, "M15": 900, "H1": 3600}[b.Timeframe]
	if seconds == 0 || b.CurrentBarTime <= 0 || len(b.Candles) == 0 || len(b.Candles) > 200 {
		return fmt.Errorf("invalid timeframe or batch size")
	}
	if b.Forming != nil {
		c := b.Forming
		if c.Time != b.CurrentBarTime || c.Open <= 0 || c.Close <= 0 || c.Low <= 0 || c.High < c.Low || c.High < c.Open || c.High < c.Close || c.Low > c.Open || c.Low > c.Close || c.TickVolume < 0 {
			return fmt.Errorf("invalid forming candle")
		}
	}
	var previous int64
	for _, c := range b.Candles {
		if c.Time <= previous || c.Time+seconds > b.CurrentBarTime || c.Open <= 0 || c.Close <= 0 || c.Low <= 0 || c.High < c.Low || c.High < c.Open || c.High < c.Close || c.Low > c.Open || c.Low > c.Close || c.TickVolume < 0 {
			return fmt.Errorf("invalid OHLC or unclosed/unordered candle")
		}
		previous = c.Time
	}
	return nil
}

type storedCandles struct {
	CandleBatch
	ReceivedAt time.Time `json:"received_at"`
	Restored   bool      `json:"restored"`
}

func candleRoutes(api *gin.RouterGroup, db *candleDatabase, symbols ...string) {
	symbol := selectedSymbol(symbols)
	pin, _ := loadExecutionPin()
	cfg, err := strategyConfig(symbol)
	if err != nil {
		panic(err)
	}
	var lastSignal *Signal
	signalState := "waiting_for_m3"
	var evaluatedAt time.Time
	var signalWindowStarted time.Time
	var mu sync.RWMutex
	batches := map[string]storedCandles{}
	if db != nil {
		var err error
		batches, err = db.load(symbol)
		if err != nil {
			panic(err)
		}
	}
	api.POST("/bridge/candles", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
		d := json.NewDecoder(c.Request.Body)
		d.DisallowUnknownFields()
		var b CandleBatch
		if err := d.Decode(&b); err != nil {
			c.JSON(400, gin.H{"error": "invalid candle JSON"})
			return
		}
		if d.Decode(new(any)) != io.EOF {
			c.JSON(400, gin.H{"error": "expected one object"})
			return
		}
		if b.Symbol != symbol {
			c.AbortWithStatus(400)
			return
		}
		if err := b.validate(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if old, ok := batches[b.Timeframe]; ok && b.CurrentBarTime < old.CurrentBarTime {
			c.JSON(409, gin.H{"error": "older candle batch"})
			return
		}
		received := time.Now()
		if db != nil {
			if err := db.save(c.Request.Context(), b, received); err != nil {
				c.JSON(503, gin.H{"error": "candle storage unavailable; retry upload"})
				return
			}
		}
		if b.Timeframe == "M3" {
			if old, ok := batches["M3"]; !ok || old.CurrentBarTime != b.CurrentBarTime {
				signalWindowStarted = received
			}
			signal, state := breakout(b, cfg)
			if signal != nil && db != nil {
				if err := db.saveSignal(c.Request.Context(), signal); err != nil {
					c.JSON(503, gin.H{"error": "signal storage unavailable"})
					return
				}
			}
			lastSignal = signal
			signalState = state
			evaluatedAt = received
		}
		batches[b.Timeframe] = storedCandles{CandleBatch: b, ReceivedAt: received}
		c.JSON(200, gin.H{"accepted": len(b.Candles)})
	})
	// Bearer-only machine endpoint. Broker-clock freshness and account checks
	// are independently enforced by the demo executor before sending orders.
	api.GET("/bridge/intent", func(c *gin.Context) {
		mu.RLock()
		defer mu.RUnlock()
		c.Header("Cache-Control", "no-store")
		if db != nil {
			control, err := db.tradingControl(c.Request.Context(), symbol)
			if err != nil {
				c.Status(503)
				return
			}
			// A resumed pair can use only a newly evaluated signal, never one
			// that was already pending when the owner changed permission.
			if !controlAllowsSignal(control, signalWindowStarted) {
				c.Status(204)
				return
			}
			acknowledged, err := db.controlAcknowledged(c.Request.Context(), pin, control)
			if err != nil {
				c.Status(503)
				return
			}
			if !acknowledged {
				c.Status(204)
				return
			}
		}
		if lastSignal == nil || evaluatedAt.IsZero() || time.Since(evaluatedAt) > 10*time.Second {
			c.Status(http.StatusNoContent)
			return
		}
		c.String(200, "%d,%s,%.8f,%d,%.8f,%.8f,%.8f", lastSignal.CandleTime, lastSignal.Side, lastSignal.Stop, cfg.Lookback, cfg.StopBuffer, lastSignal.TP1, lastSignal.TP2)
	})
	api.GET("/strategy", func(c *gin.Context) {
		mu.RLock()
		defer mu.RUnlock()
		state := signalState
		if !evaluatedAt.IsZero() && time.Since(evaluatedAt) > 10*time.Second {
			state = "stale_delivery"
		}
		c.JSON(200, gin.H{"mode": "signals_only", "state": state, "config": cfg, "evaluated_at": evaluatedAt, "signal": lastSignal, "entry_basis": "closed candle price, not an executed fill"})
	})
	api.GET("/candles", func(c *gin.Context) {
		tf := c.DefaultQuery("timeframe", "M15")
		if tf != "M3" && tf != "M5" && tf != "M15" && tf != "H1" {
			c.JSON(400, gin.H{"error": "use M3, M5, M15 or H1"})
			return
		}
		mu.RLock()
		defer mu.RUnlock()
		b, ok := batches[tf]
		if !ok {
			c.JSON(200, gin.H{"state": "waiting_for_candles", "candles": []Candle{}})
			return
		}
		state := "received"
		if b.Restored || time.Since(b.ReceivedAt) > 90*time.Second {
			state = "stale_delivery"
		}
		c.JSON(200, gin.H{"state": state, "data": b, "time_basis": "MT5 broker time; not normalized to UTC", "closed_only": b.Forming == nil})
	})
}
