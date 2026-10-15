package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// Only the dedicated loopback listener behind Tailscale Serve sets this context.
// Serve strips client identity headers. Local processes on the VPS are trusted;
// never expose this listener or forward arbitrary public traffic to it.
type dashboardIngressKey struct{}

func privateDashboardHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			http.Error(w, "forbidden", 403)
			return
		}
		if strings.Contains(r.URL.Path, "/bridge/") {
			http.Error(w, "forbidden", 403)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), dashboardIngressKey{}, true)))
	})
}

type controlAuth struct{ host, owner, secret, csrf string }

func newControlAuth(host, owner string) *controlAuth {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return &controlAuth{host: host, owner: owner, secret: hex.EncodeToString(b[:32]), csrf: hex.EncodeToString(b[32:])}
}
func (a *controlAuth) identity(r *http.Request) bool {
	trusted, _ := r.Context().Value(dashboardIngressKey{}).(bool)
	return trusted && a.owner != "" && a.host != "" && (r.Host == a.host || r.Host == a.host+":443") && r.Header.Get("Tailscale-User-Login") == a.owner
}
func (a *controlAuth) sign(value string) string {
	h := hmac.New(sha256.New, []byte(a.secret))
	h.Write([]byte(a.owner + ":" + value))
	return hex.EncodeToString(h.Sum(nil))
}
func (a *controlAuth) issue(c *gin.Context) {
	if !a.identity(c.Request) {
		return
	}
	expiry := strconv.FormatInt(time.Now().Add(8*time.Hour).Unix(), 10)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("gold_control", expiry+"."+a.sign(expiry), 28800, "/api/controls", "", true, true)
}
func (a *controlAuth) authorized(c *gin.Context) bool {
	if !a.identity(c.Request) {
		return false
	}
	value, _ := c.Cookie("gold_control")
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	return err == nil && time.Now().Unix() < expiry && hmac.Equal([]byte(parts[1]), []byte(a.sign(parts[0])))
}

type TradingControl struct {
	Symbol    string    `json:"symbol"`
	Enabled   bool      `json:"enabled"`
	Revision  int64     `json:"revision"`
	ChangedAt time.Time `json:"changed_at"`
	ChangedBy string    `json:"changed_by"`
}

func (db *candleDatabase) tradingControl(ctx context.Context, symbols ...string) (s TradingControl, err error) {
	if db == nil {
		return s, errors.New("database unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = db.pool.QueryRow(ctx, "SELECT symbol,enabled,revision,changed_at,changed_by FROM trading_controls WHERE symbol=$1", selectedSymbol(symbols)).Scan(&s.Symbol, &s.Enabled, &s.Revision, &s.ChangedAt, &s.ChangedBy)
	return
}

var errControlConflict = errors.New("control changed; refresh")

func (db *candleDatabase) changeControl(ctx context.Context, enabled bool, revision int64, actor string, symbols ...string) error {
	if db == nil {
		return errors.New("database unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var next int64
	err = tx.QueryRow(ctx, `UPDATE trading_controls SET enabled=$1,revision=revision+1,changed_at=now(),changed_by=$2 WHERE symbol=$4 AND revision=$3 RETURNING revision`, enabled, actor, revision, selectedSymbol(symbols)).Scan(&next)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errControlConflict
		}
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO trading_control_audit(symbol,enabled,revision,actor) VALUES($4,$1,$2,$3)", enabled, next, actor, selectedSymbol(symbols))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func controlRoutes(r *gin.Engine, api *gin.RouterGroup, db *candleDatabase, a *controlAuth, symbols ...string) {
	symbol := selectedSymbol(symbols)
	group := r.Group("/api/controls")
	group.Use(func(c *gin.Context) {
		if !a.authorized(c) {
			c.AbortWithStatusJSON(403, gin.H{"error": "Open the private dashboard as its owner, or refresh to renew your session."})
			return
		}
		c.Next()
	})
	group.GET("/"+symbol, func(c *gin.Context) {
		s, err := db.tradingControl(c.Request.Context(), symbol)
		if err != nil {
			c.JSON(503, gin.H{"error": "Trading controls unavailable"})
			return
		}
		c.JSON(200, gin.H{"control": s, "csrf": a.csrf})
	})
	group.POST("/"+symbol, func(c *gin.Context) {
		if c.GetHeader("Origin") != "https://"+a.host || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(a.csrf)) != 1 {
			c.AbortWithStatus(403)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var v struct {
			Enabled  *bool `json:"enabled"`
			Revision int64 `json:"revision"`
		}
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&v) != nil || dec.Decode(new(any)) != io.EOF || v.Enabled == nil || v.Revision < 1 {
			c.AbortWithStatus(400)
			return
		}
		err := db.changeControl(c.Request.Context(), *v.Enabled, v.Revision, a.owner, symbol)
		if errors.Is(err, errControlConflict) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if err != nil {
			c.JSON(503, gin.H{"error": "Could not save trading permission"})
			return
		}
		c.JSON(200, gin.H{"saved": true})
	})
	api.GET("/bridge/control", func(c *gin.Context) {
		s, err := db.tradingControl(c.Request.Context(), symbol)
		if err != nil {
			c.Status(503)
			return
		}
		enabled := 0
		if s.Enabled {
			enabled = 1
		}
		c.String(200, "%d,%d", s.Revision, enabled)
	})
}
func configuredControlAuth(host string) *controlAuth {
	b, _ := os.ReadFile(".local/dashboard-owner")
	return newControlAuth(host, strings.TrimSpace(string(b)))
}

func controlAllowsSignal(s TradingControl, firstSeen time.Time) bool {
	return s.Enabled && !firstSeen.IsZero() && firstSeen.After(s.ChangedAt)
}
func (db *candleDatabase) controlAcknowledged(ctx context.Context, pin executionPin, s TradingControl) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var ok bool
	err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_snapshots WHERE account=$1 AND server=$2 AND symbol=$5 AND received_at>now()-interval '10 seconds' AND payload @> jsonb_build_object('control_revision',$3::bigint,'control_enabled',$4::boolean,'control_ok',true,'connected',true))`, pin.Account, pin.Server, s.Revision, s.Enabled, s.Symbol).Scan(&ok)
	return ok, err
}
