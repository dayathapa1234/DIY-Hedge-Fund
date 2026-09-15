//go:build integration

package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"market-data-service/internal/marketdata"
)

func TestPostgresPersistsWatchedPrices(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}

	ctx := context.Background()
	store, err := Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM price_bars WHERE symbol = 'MDS_TEST'`)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM watched_symbols WHERE symbol = 'MDS_TEST'`)
		store.Close()
	})

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	query := marketdata.HistoricalQuery{Symbol: "MDS_TEST", Interval: "1d", Provider: "test"}
	_, _ = store.pool.Exec(ctx, `DELETE FROM price_bars WHERE symbol = $1`, query.Symbol)
	_, _ = store.pool.Exec(ctx, `DELETE FROM watched_symbols WHERE symbol = $1`, query.Symbol)
	if err := store.MarkWatched(ctx, query); err != nil {
		t.Fatal(err)
	}

	result := marketdata.HistoricalResult{
		Symbol:   query.Symbol,
		Provider: "test",
		Interval: "1d",
		Prices: []marketdata.PriceBar{{
			Date:     time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			Open:     1,
			High:     2,
			Low:      1,
			Close:    2,
			AdjClose: 2,
			Volume:   100,
		}},
	}
	if err := store.UpsertPrices(ctx, result); err != nil {
		t.Fatal(err)
	}

	stored, err := store.GetPrices(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Prices) == 0 {
		t.Fatal("expected persisted prices")
	}
}
