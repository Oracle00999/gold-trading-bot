ALTER TABLE trading_controls DROP CONSTRAINT trading_controls_symbol_check;
ALTER TABLE trading_controls ADD CHECK (symbol IN ('XAUUSDm','EURUSDm','USDJPYm'));
INSERT INTO trading_controls(symbol) VALUES ('USDJPYm');
