package marketdata

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeProvider struct {
	name   string
	prices []PriceBar
	err    error
}

type fakeStore struct {
	marked []HistoricalQuery
}

func (f fakeProvider) Name() string {
	return f.name
}

func (f fakeProvider) Historical(context.Context, HistoricalQuery) (HistoricalResult, error) {
	if f.err != nil {
		return HistoricalResult{}, f.err
	}
	return HistoricalResult{Symbol: "AAPL", Provider: f.name, Prices: f.prices}, nil
}

func (f *fakeStore) Migrate(context.Context) error { return nil }

func (f *fakeStore) Ping(context.Context) error { return nil }

func (f *fakeStore) Close() {}

func (f *fakeStore) GetPrices(context.Context, HistoricalQuery) (HistoricalResult, error) {
	return HistoricalResult{}, ErrNoData
}

func (f *fakeStore) UpsertPrices(context.Context, HistoricalResult) error { return nil }

func (f *fakeStore) MarkWatched(_ context.Context, query HistoricalQuery) error {
	f.marked = append(f.marked, query)
	return nil
}

func (f *fakeStore) ListWatched(context.Context) ([]HistoricalQuery, error) {
	return f.marked, nil
}

func TestNormalizeInterval(t *testing.T) {
	tests := map[string]string{
		"":        "1d",
		"daily":   "1d",
		"weekly":  "1wk",
		"monthly": "1mo",
		"hourly":  "1h",
		"15m":     "15m",
	}
	for input, expected := range tests {
		actual, err := NormalizeInterval(input)
		if err != nil {
			t.Fatalf("expected %q to normalize: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("expected %q to normalize to %q, got %q", input, expected, actual)
		}
	}

	if _, err := NormalizeInterval("yearly"); err == nil {
		t.Fatal("expected invalid interval to fail")
	}
}

func TestHistoricalFallsBackToNextProvider(t *testing.T) {
	registry := NewRegistry()
	registry.Register(fakeProvider{name: "first", err: ErrNoData})
	registry.Register(fakeProvider{name: "second", prices: []PriceBar{{Date: mustDate(t, "2024-01-02"), Close: 100}}})
	service := NewService(registry, []string{"first", "second"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := service.Historical(context.Background(), HistoricalQuery{Symbol: "aapl"})
	if err != nil {
		t.Fatalf("expected fallback to succeed: %v", err)
	}
	if result.Provider != "second" {
		t.Fatalf("expected second provider, got %q", result.Provider)
	}
	if result.Symbol != "AAPL" {
		t.Fatalf("expected normalized symbol AAPL, got %q", result.Symbol)
	}
	if result.Interval != "1d" {
		t.Fatalf("expected default interval 1d, got %q", result.Interval)
	}
}

func TestHistoricalForcedProviderDoesNotFallback(t *testing.T) {
	registry := NewRegistry()
	registry.Register(fakeProvider{name: "first", err: errors.New("down")})
	registry.Register(fakeProvider{name: "second", prices: []PriceBar{{Date: mustDate(t, "2024-01-02"), Close: 100}}})
	service := NewService(registry, []string{"first", "second"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := service.Historical(context.Background(), HistoricalQuery{Symbol: "AAPL", Provider: "first"})
	if err == nil {
		t.Fatal("expected forced provider failure")
	}
}

func TestHistoricalFiltersInclusiveDateRange(t *testing.T) {
	registry := NewRegistry()
	registry.Register(fakeProvider{name: "test", prices: []PriceBar{
		{Date: mustDate(t, "2023-12-29"), Close: 99},
		{Date: mustDate(t, "2024-01-02"), Close: 100},
		{Date: mustDate(t, "2024-01-03"), Close: 101},
		{Date: mustDate(t, "2024-02-01"), Close: 102},
	}})
	service := NewService(registry, []string{"test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := service.Historical(context.Background(), HistoricalQuery{
		Symbol: "AAPL",
		From:   mustDate(t, "2024-01-02"),
		To:     mustDate(t, "2024-01-03"),
	})
	if err != nil {
		t.Fatalf("expected range to succeed: %v", err)
	}
	if len(result.Prices) != 2 {
		t.Fatalf("expected 2 prices, got %d", len(result.Prices))
	}
}

func TestListSymbolsFiltersSortsAndPaginates(t *testing.T) {
	service := NewService(NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := service.ListSymbols(context.Background(), SymbolListQuery{Text: "a", Exchange: "nasdaq", Sort: "symbol", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("expected list symbols to succeed: %v", err)
	}
	if result.Total != 7 {
		t.Fatalf("expected 7 matching symbols, got %d", result.Total)
	}
	if result.Limit != 2 || result.Offset != 1 || len(result.Results) != 2 {
		t.Fatalf("unexpected pagination result: %+v", result)
	}
	if result.Results[0].Symbol != "AMZN" || result.Results[1].Symbol != "GOOGL" {
		t.Fatalf("unexpected page: %+v", result.Results)
	}
}

func TestListSymbolsSortsByNameDescending(t *testing.T) {
	service := NewService(NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := service.ListSymbols(context.Background(), SymbolListQuery{Sort: "name", Order: "desc", Limit: 3})
	if err != nil {
		t.Fatalf("expected list symbols to succeed: %v", err)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(result.Results))
	}
	if result.Results[0].Symbol != "TSLA" || result.Results[1].Symbol != "SPY" || result.Results[2].Symbol != "NVDA" {
		t.Fatalf("unexpected sorted page: %+v", result.Results)
	}
}

func TestListSymbolsRejectsInvalidSort(t *testing.T) {
	service := NewService(NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := service.ListSymbols(context.Background(), SymbolListQuery{Sort: "price"})
	if err == nil {
		t.Fatal("expected invalid sort to fail")
	}
}

func TestListSymbolsRejectsNegativeOffset(t *testing.T) {
	service := NewService(NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := service.ListSymbols(context.Background(), SymbolListQuery{Offset: -1})
	if err == nil {
		t.Fatal("expected negative offset to fail")
	}
}

func TestAddSymbolIncludesSymbolInListAndSearch(t *testing.T) {
	service := NewService(NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	added, err := service.AddSymbol(context.Background(), SymbolSearchResult{Symbol: "ibm", Name: "International Business Machines", Exchange: "nyse", Currency: "usd"})
	if err != nil {
		t.Fatalf("expected add symbol to succeed: %v", err)
	}
	if added.Symbol != "IBM" || added.Exchange != "NYSE" || added.Currency != "USD" || added.Provider != "local" {
		t.Fatalf("unexpected normalized symbol: %+v", added)
	}

	listed, err := service.ListSymbols(context.Background(), SymbolListQuery{Text: "ibm"})
	if err != nil {
		t.Fatalf("expected list symbols to succeed: %v", err)
	}
	if listed.Total != 1 || listed.Results[0].Symbol != "IBM" {
		t.Fatalf("expected added symbol in list: %+v", listed)
	}

	searched, err := service.SearchSymbols(context.Background(), "business")
	if err != nil {
		t.Fatalf("expected search symbols to succeed: %v", err)
	}
	found := false
	for _, result := range searched {
		if result.Symbol == "IBM" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected added symbol in search: %+v", searched)
	}
}

func TestAddSymbolMarksSymbolWatchedForFeed(t *testing.T) {
	store := &fakeStore{}
	service := NewService(NewRegistry(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.SetStore(store)

	_, err := service.AddSymbol(context.Background(), SymbolSearchResult{Symbol: "ibm", Name: "International Business Machines", Exchange: "nyse", Currency: "usd"})
	if err != nil {
		t.Fatalf("expected add symbol to succeed: %v", err)
	}
	if len(store.marked) != 1 {
		t.Fatalf("expected symbol to be marked watched, got %+v", store.marked)
	}
	if store.marked[0].Symbol != "IBM" || store.marked[0].Interval != "1d" || store.marked[0].Provider != "" {
		t.Fatalf("unexpected watched query: %+v", store.marked[0])
	}
}

func mustDate(t *testing.T, value string) time.Time {
	t.Helper()
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
