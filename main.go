package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed dashboard.html
var dashboard []byte

// Snapshot is telemetry only. No order commands are supported in this milestone.
type Snapshot struct {
	Demo       bool    `json:"demo"`
	Connected  bool    `json:"connected"`
	Currency   string  `json:"currency"`
	Balance    float64 `json:"balance"`
	Equity     float64 `json:"equity"`
	Symbol     string  `json:"symbol"`
	Bid        float64 `json:"bid"`
	Ask        float64 `json:"ask"`
	TickTimeMS int64   `json:"tick_time_ms"`
}

func router(token string, databases ...*candleDatabase) *gin.Engine {
	var db *candleDatabase
	if len(databases) > 0 {
		db = databases[0]
	}
	r := gin.New()
	r.Use(gin.Recovery())
	// Restrict browser access to the intended loopback host (including DNS rebinding).
	r.Use(func(c *gin.Context) {
		if c.Request.Host != "127.0.0.1:8080" && c.Request.Host != "localhost:8080" && c.Request.Host != "127.0.0.1" && c.Request.Host != "localhost" {
			c.AbortWithStatus(403)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	})
	readKey := make([]byte, 32)
	if _, err := rand.Read(readKey); err != nil {
		panic(err)
	}
	readToken := hex.EncodeToString(readKey)
	r.GET("/", func(c *gin.Context) {
		c.SetSameSite(http.SameSiteStrictMode)
		// Remove the legacy narrower cookie, which otherwise shadows /api.
		c.SetCookie("dashboard", "", -1, "/api/status", "", false, true)
		c.SetCookie("dashboard", readToken, 0, "/api", "", false, true)
		c.Data(200, "text/html; charset=utf-8", dashboard)
	})
	_ = r.SetTrustedProxies(nil)
	var mu sync.RWMutex
	var latest *Snapshot
	var received time.Time
	var tickChanged time.Time
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	api := r.Group("/api")
	api.Use(func(c *gin.Context) {
		cookie, _ := c.Cookie("dashboard")
		if c.Request.Method == http.MethodGet && (c.Request.URL.Path == "/api/status" || c.Request.URL.Path == "/api/candles" || c.Request.URL.Path == "/api/strategy" || c.Request.URL.Path == "/api/execution") && subtle.ConstantTimeCompare([]byte(cookie), []byte(readToken)) == 1 {
			c.Next()
			return
		}
		supplied := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	})
	candleRoutes(api, db)
	executionRoutes(api, db)
	api.POST("/bridge/snapshot", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var s Snapshot
		if err := decoder.Decode(&s); err != nil {
			c.JSON(400, gin.H{"error": "invalid snapshot"})
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			c.JSON(400, gin.H{"error": "expected one JSON object"})
			return
		}
		if !s.Demo {
			c.JSON(403, gin.H{"error": "demo accounts only"})
			return
		}
		if s.Symbol != "XAUUSDm" || len(s.Currency) != 3 || s.Bid <= 0 || s.Ask < s.Bid || s.TickTimeMS <= 0 {
			c.JSON(400, gin.H{"error": "invalid symbol, currency, or quote"})
			return
		}
		mu.Lock()
		now := time.Now()
		if latest == nil || latest.TickTimeMS != s.TickTimeMS {
			tickChanged = now
		}
		latest, received = &s, now
		mu.Unlock()
		c.JSON(200, gin.H{"accepted": true})
	})
	api.GET("/status", func(c *gin.Context) {
		mu.RLock()
		defer mu.RUnlock()
		if latest == nil {
			c.JSON(200, gin.H{"state": "waiting_for_mt5", "trading_enabled": nil, "execution_status": "check_mt5_executor"})
			return
		}
		state := "connected"
		if time.Since(received) > 10*time.Second || !latest.Connected {
			state = "disconnected"
		} else if time.Since(tickChanged) > 30*time.Second {
			state = "stale_quote"
		}
		c.JSON(200, gin.H{"state": state, "trading_enabled": nil, "execution_status": "check_mt5_executor", "received_at": received, "snapshot": latest})
	})
	return r
}

func localToken() (string, error) {
	if err := os.MkdirAll(".local", 0700); err != nil {
		return "", err
	}
	path := ".local/bridge-token"
	b, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(b))
		if len(token) != 64 {
			return "", errors.New("invalid local bridge token")
		}
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	b = make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	err = os.WriteFile(path, []byte(token+"\n"), 0600)
	return token, err
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "tick-backtest" {
		if err := tickBacktestCLI(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "import-history" {
		if err := importHistoryCLI(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "backtest" {
		if err := runBacktestCLI(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if _, err := loadExecutionPin(); err != nil {
		log.Fatal(err)
	}
	token, err := localToken()
	if err != nil {
		log.Fatal(err)
	}
	db, err := openDatabase()
	if err != nil {
		log.Fatal("Database unavailable; start PostgreSQL with docker compose. ", err)
	}
	defer db.pool.Close()
	gin.SetMode(gin.ReleaseMode)
	server := &http.Server{Addr: "127.0.0.1:8080", Handler: router(token, db), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Print("Listening on http://127.0.0.1:8080; bridge token saved in .local/bridge-token")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Print(err)
	}
}
