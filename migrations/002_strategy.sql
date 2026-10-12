ALTER TABLE candles DROP CONSTRAINT IF EXISTS candles_timeframe_check;
ALTER TABLE candles ADD CONSTRAINT candles_timeframe_check CHECK (timeframe IN ('M3','M5','M15','H1'));
CREATE TABLE breakout_signals (
 symbol text NOT NULL,
 candle_time bigint NOT NULL,
 lookback integer NOT NULL,
 stop_buffer double precision NOT NULL,
 payload jsonb NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(symbol,candle_time,lookback,stop_buffer)
);
