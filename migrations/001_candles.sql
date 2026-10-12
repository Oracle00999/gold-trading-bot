CREATE TABLE IF NOT EXISTS candles (
 symbol text NOT NULL,
 timeframe text NOT NULL CHECK (timeframe IN ('M5','M15','H1')),
 broker_time bigint NOT NULL,
 open double precision NOT NULL,
 high double precision NOT NULL,
 low double precision NOT NULL,
 close double precision NOT NULL,
 tick_volume bigint NOT NULL CHECK (tick_volume >= 0),
 PRIMARY KEY(symbol,timeframe,broker_time),
 CHECK(low > 0 AND high >= low AND open BETWEEN low AND high AND close BETWEEN low AND high)
);
CREATE TABLE IF NOT EXISTS candle_windows (
 symbol text NOT NULL,
 timeframe text NOT NULL,
 current_bar_time bigint NOT NULL,
 received_at timestamptz NOT NULL,
 PRIMARY KEY(symbol,timeframe)
);
