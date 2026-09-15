package storage

import (
	"context"
	"errors"
	"market-data-service/internal/marketdata"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func Connect(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return NewPostgres(pool), nil
}

func (p *Postgres) Close() {
	p.pool.Close()
}

func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
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
`)
	return err
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *Postgres) GetPrices(ctx context.Context, query marketdata.HistoricalQuery) (marketdata.HistoricalResult, error) {
	rows, err := p.pool.Query(ctx, `
SELECT symbol, interval, ts, provider, open, high, low, close, adj_close, volume
FROM (
  SELECT DISTINCT ON (ts)
    symbol,
    interval,
    ts,
    provider,
    open,
    high,
    low,
    close,
    COALESCE(adj_close, close) AS adj_close,
    COALESCE(volume, 0) AS volume,
    updated_at
  FROM price_bars
  WHERE symbol = $1
    AND interval = $2
    AND ($3::timestamptz IS NULL OR ts >= $3)
    AND ($4::timestamptz IS NULL OR ts <= $4)
    AND ($5::text = '' OR provider = $5)
  ORDER BY ts ASC, updated_at DESC
) bars
ORDER BY ts ASC
`, query.Symbol, query.Interval, nullTime(query.From), nullTime(query.To), query.Provider)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	defer rows.Close()

	return scanPriceRows(rows)
}

func scanPriceRows(rows pgx.Rows) (marketdata.HistoricalResult, error) {
	var result marketdata.HistoricalResult
	for rows.Next() {
		var price marketdata.PriceBar
		if err := rows.Scan(&result.Symbol, &result.Interval, &price.Date, &result.Provider, &price.Open, &price.High, &price.Low, &price.Close, &price.AdjClose, &price.Volume); err != nil {
			return marketdata.HistoricalResult{}, err
		}
		result.Prices = append(result.Prices, price)
	}
	if err := rows.Err(); err != nil {
		return marketdata.HistoricalResult{}, err
	}
	if len(result.Prices) == 0 {
		return marketdata.HistoricalResult{}, marketdata.ErrNoData
	}
	return result, nil
}

func (p *Postgres) UpsertPrices(ctx context.Context, result marketdata.HistoricalResult) error {
	batch := &pgx.Batch{}
	for _, price := range result.Prices {
		batch.Queue(`
INSERT INTO price_bars (symbol, interval, ts, provider, open, high, low, close, adj_close, volume)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (symbol, interval, ts, provider) DO UPDATE SET
  open = EXCLUDED.open,
  high = EXCLUDED.high,
  low = EXCLUDED.low,
  close = EXCLUDED.close,
  adj_close = EXCLUDED.adj_close,
  volume = EXCLUDED.volume,
  updated_at = now()
`, result.Symbol, result.Interval, price.Date, result.Provider, price.Open, price.High, price.Low, price.Close, price.AdjClose, price.Volume)
	}
	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range result.Prices {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	_, err := p.pool.Exec(ctx, `UPDATE watched_symbols SET last_refreshed_at = now() WHERE symbol = $1 AND interval = $2`, result.Symbol, result.Interval)
	return err
}

func (p *Postgres) MarkWatched(ctx context.Context, query marketdata.HistoricalQuery) error {
	provider := query.Provider
	_, err := p.pool.Exec(ctx, `
INSERT INTO watched_symbols (symbol, interval, provider)
VALUES ($1,$2,$3)
ON CONFLICT (symbol, interval, provider) DO UPDATE SET last_requested_at = now()
`, query.Symbol, query.Interval, provider)
	return err
}

func (p *Postgres) ListWatched(ctx context.Context) ([]marketdata.HistoricalQuery, error) {
	rows, err := p.pool.Query(ctx, `SELECT symbol, interval, provider FROM watched_symbols ORDER BY last_requested_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []marketdata.HistoricalQuery
	for rows.Next() {
		var query marketdata.HistoricalQuery
		if err := rows.Scan(&query.Symbol, &query.Interval, &query.Provider); err != nil {
			return nil, err
		}
		queries = append(queries, query)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return queries, nil
}

func nullTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
