package main

import (
	"context"
	_ "embed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"time"
)

//go:embed migrations/001_candles.sql
var candleMigration string

//go:embed migrations/002_strategy.sql
var strategyMigration string

//go:embed migrations/003_execution.sql
var executionMigration string

//go:embed migrations/004_trading_controls.sql
var controlMigration string

//go:embed migrations/005_eurusd.sql
var euroMigration string

//go:embed migrations/006_usdjpy.sql
var yenMigration string

type candleDatabase struct{ pool *pgxpool.Pool }

func openDatabase() (*candleDatabase, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		b, err := os.ReadFile(".local/database-url")
		if err != nil {
			return nil, err
		}
		url = strings.TrimSpace(string(b))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	db := &candleDatabase{pool}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(780021); CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY);`)
	if err == nil {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=1)").Scan(&exists)
		if err == nil && !exists {
			_, err = tx.Exec(ctx, candleMigration)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES(1)")
			}
		}
	}
	if err == nil {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=2)").Scan(&exists)
		if err == nil && !exists {
			_, err = tx.Exec(ctx, strategyMigration)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES(2)")
			}
		}
	}
	if err == nil {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=3)").Scan(&exists)
		if err == nil && !exists {
			_, err = tx.Exec(ctx, executionMigration)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES(3)")
			}
		}
	}
	if err == nil {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=4)").Scan(&exists)
		if err == nil && !exists {
			_, err = tx.Exec(ctx, controlMigration)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES(4)")
			}
		}
	}
	if err == nil {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=5)").Scan(&exists)
		if err == nil && !exists {
			_, err = tx.Exec(ctx, euroMigration)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES(5)")
			}
		}
	}
	if err == nil {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=6)").Scan(&exists)
		if err == nil && !exists {
			_, err = tx.Exec(ctx, yenMigration)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES(6)")
			}
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}
func (db *candleDatabase) save(ctx context.Context, b CandleBatch, received time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for _, c := range b.Candles {
		batch.Queue(`INSERT INTO candles VALUES($1,$2,$3,$4,$5,$6,$7,$8)
 ON CONFLICT(symbol,timeframe,broker_time) DO UPDATE SET open=EXCLUDED.open,high=EXCLUDED.high,low=EXCLUDED.low,close=EXCLUDED.close,tick_volume=EXCLUDED.tick_volume
 WHERE (candles.open,candles.high,candles.low,candles.close,candles.tick_volume) IS DISTINCT FROM (EXCLUDED.open,EXCLUDED.high,EXCLUDED.low,EXCLUDED.close,EXCLUDED.tick_volume)`, b.Symbol, b.Timeframe, c.Time, c.Open, c.High, c.Low, c.Close, c.TickVolume)
	}
	batch.Queue(`INSERT INTO candle_windows VALUES($1,$2,$3,$4) ON CONFLICT(symbol,timeframe) DO UPDATE SET current_bar_time=EXCLUDED.current_bar_time,received_at=EXCLUDED.received_at`, b.Symbol, b.Timeframe, b.CurrentBarTime, received)
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (db *candleDatabase) load(symbols ...string) (map[string]storedCandles, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out := map[string]storedCandles{}
	for _, tf := range []string{"M3", "M5", "M15", "H1"} {
		b := storedCandles{CandleBatch: CandleBatch{Demo: true, Symbol: selectedSymbol(symbols), Timeframe: tf}, Restored: true}
		err := db.pool.QueryRow(ctx, "SELECT current_bar_time,received_at FROM candle_windows WHERE symbol=$1 AND timeframe=$2", b.Symbol, tf).Scan(&b.CurrentBarTime, &b.ReceivedAt)
		if err == pgx.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		rows, err := db.pool.Query(ctx, `SELECT broker_time,open,high,low,close,tick_volume FROM (SELECT * FROM candles WHERE symbol=$1 AND timeframe=$2 AND broker_time<$3 ORDER BY broker_time DESC LIMIT 200) c ORDER BY broker_time`, b.Symbol, tf, b.CurrentBarTime)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c Candle
			if err = rows.Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.TickVolume); err != nil {
				rows.Close()
				return nil, err
			}
			b.Candles = append(b.Candles, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out[tf] = b
	}
	return out, nil
}
