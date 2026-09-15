CREATE TABLE IF NOT EXISTS watched_symbols (
  symbol TEXT NOT NULL,
  interval TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT '',
  first_requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_refreshed_at TIMESTAMPTZ,
  PRIMARY KEY (symbol, interval, provider)
);

CREATE TABLE IF NOT EXISTS price_bars (
  symbol TEXT NOT NULL,
  interval TEXT NOT NULL,
  ts TIMESTAMPTZ NOT NULL,
  provider TEXT NOT NULL,
  open DOUBLE PRECISION NOT NULL,
  high DOUBLE PRECISION NOT NULL,
  low DOUBLE PRECISION NOT NULL,
  close DOUBLE PRECISION NOT NULL,
  adj_close DOUBLE PRECISION,
  volume BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (symbol, interval, ts, provider)
);

CREATE INDEX IF NOT EXISTS price_bars_symbol_interval_ts_idx ON price_bars (symbol, interval, ts);
