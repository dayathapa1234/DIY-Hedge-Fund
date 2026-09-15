//go:build integration

package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"market-data-service/internal/marketdata"
)

func TestRedisCacheRoundTrip(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR is required")
	}

	cache := NewRedis(addr, os.Getenv("TEST_REDIS_PASSWORD"), 0, time.Minute)
	defer cache.Close()
	if err := cache.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	query := marketdata.HistoricalQuery{Symbol: "AAPL", Interval: "1d"}
	result := marketdata.HistoricalResult{Symbol: "AAPL", Provider: "test", Interval: "1d", Prices: []marketdata.PriceBar{{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100}}}
	cache.Set(query, result)

	stored, ok := cache.Get(query)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if stored.Symbol != "AAPL" || len(stored.Prices) != 1 {
		t.Fatalf("unexpected cached value: %+v", stored)
	}
}
