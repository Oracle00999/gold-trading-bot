CREATE TABLE trading_controls (
 symbol text PRIMARY KEY CHECK (symbol = 'XAUUSDm'),
 enabled boolean NOT NULL DEFAULT false,
 revision bigint NOT NULL DEFAULT 1,
 changed_at timestamptz NOT NULL DEFAULT now(),
 changed_by text NOT NULL DEFAULT 'initial setup'
);
INSERT INTO trading_controls(symbol) VALUES ('XAUUSDm');
CREATE TABLE trading_control_audit (
 id bigserial PRIMARY KEY,
 symbol text NOT NULL,
 enabled boolean NOT NULL,
 revision bigint NOT NULL,
 actor text NOT NULL,
 changed_at timestamptz NOT NULL DEFAULT now()
);
