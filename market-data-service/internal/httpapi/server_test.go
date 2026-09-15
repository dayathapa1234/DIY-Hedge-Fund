package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"market-data-service/internal/marketdata"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type apiFakeProvider struct {
	lastQuery marketdata.HistoricalQuery
}

func (p *apiFakeProvider) Name() string {
	return "fake"
}

func (p *apiFakeProvider) Historical(ctx context.Context, query marketdata.HistoricalQuery) (marketdata.HistoricalResult, error) {
	p.lastQuery = query
	return marketdata.HistoricalResult{
		Symbol:   query.Symbol,
		Provider: p.Name(),
		Interval: query.Interval,
		Prices: []marketdata.PriceBar{
			{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Open: 1, High: 2, Low: 1, Close: 2, AdjClose: 2, Volume: 100},
		},
	}, nil
}

func TestLivez(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/livez", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected request id header")
	}
}

func TestReadyz(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
}

func TestOpenAPIAndSwaggerEndpoints(t *testing.T) {
	server, _ := newTestServer()

	openAPI := httptest.NewRecorder()
	server.ServeHTTP(openAPI, httptest.NewRequest(http.MethodGet, "/swagger/openapi.yaml", nil))
	if openAPI.Code != http.StatusOK {
		t.Fatalf("expected openapi 200, got %d", openAPI.Code)
	}
	if !strings.Contains(openAPI.Body.String(), "openapi: 3.0.3") {
		t.Fatal("expected openapi content")
	}

	swagger := httptest.NewRecorder()
	server.ServeHTTP(swagger, httptest.NewRequest(http.MethodGet, "/swagger", nil))
	if swagger.Code != http.StatusOK {
		t.Fatalf("expected swagger 200, got %d", swagger.Code)
	}
	if !strings.Contains(swagger.Body.String(), "SwaggerUIBundle") {
		t.Fatal("expected swagger ui html")
	}
}

func TestHistoricalPricesRequiresSymbol(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/prices/historical", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestHistoricalPricesRejectsInvalidDateRange(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/prices/historical?symbol=AAPL&from=2024-02-01&to=2024-01-01", nil)
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestHistoricalPricesReturnsData(t *testing.T) {
	server, provider := newTestServer()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/prices/historical?symbol=aapl&from=2024-01-01&to=2024-01-31&interval=hourly&providers=fake", nil)
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if provider.lastQuery.Symbol != "AAPL" {
		t.Fatalf("expected normalized symbol AAPL, got %q", provider.lastQuery.Symbol)
	}
	if provider.lastQuery.Interval != "1h" {
		t.Fatalf("expected normalized interval 1h, got %q", provider.lastQuery.Interval)
	}

	var result marketdata.HistoricalResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Provider != "fake" || len(result.Prices) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Interval != "1h" {
		t.Fatalf("expected result interval 1h, got %q", result.Interval)
	}
}

func TestLatestProvidersConfigSearchAndMetrics(t *testing.T) {
	server, _ := newTestServer()

	latest := httptest.NewRecorder()
	server.ServeHTTP(latest, httptest.NewRequest(http.MethodGet, "/v1/prices/latest?symbol=AAPL", nil))
	if latest.Code != http.StatusOK {
		t.Fatalf("expected latest 200, got %d: %s", latest.Code, latest.Body.String())
	}

	providers := httptest.NewRecorder()
	server.ServeHTTP(providers, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))
	if providers.Code != http.StatusOK || !strings.Contains(providers.Body.String(), "fake") {
		t.Fatalf("expected providers response, got %d: %s", providers.Code, providers.Body.String())
	}

	config := httptest.NewRecorder()
	server.ServeHTTP(config, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if config.Code != http.StatusOK || !strings.Contains(config.Body.String(), "providerPriority") {
		t.Fatalf("expected config response, got %d: %s", config.Code, config.Body.String())
	}

	search := httptest.NewRecorder()
	server.ServeHTTP(search, httptest.NewRequest(http.MethodGet, "/v1/symbols/search?q=aapl", nil))
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), "AAPL") {
		t.Fatalf("expected search response, got %d: %s", search.Code, search.Body.String())
	}

	symbols := httptest.NewRecorder()
	server.ServeHTTP(symbols, httptest.NewRequest(http.MethodGet, "/v1/symbols?q=a&exchange=NASDAQ&sort=symbol&order=asc&limit=2&offset=1", nil))
	if symbols.Code != http.StatusOK || !strings.Contains(symbols.Body.String(), `"total":7`) || !strings.Contains(symbols.Body.String(), "AMZN") {
		t.Fatalf("expected symbols response, got %d: %s", symbols.Code, symbols.Body.String())
	}

	metrics := httptest.NewRecorder()
	server.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "market_data_requests_total") {
		t.Fatalf("expected metrics response, got %d: %s", metrics.Code, metrics.Body.String())
	}
}

func TestSymbolsRejectsInvalidPagination(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/symbols?limit=nope", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestAddSymbol(t *testing.T) {
	server, _ := newTestServer()
	add := httptest.NewRecorder()
	body := strings.NewReader(`{"symbol":"ibm","name":"International Business Machines","exchange":"nyse","currency":"usd"}`)
	server.ServeHTTP(add, httptest.NewRequest(http.MethodPost, "/v1/symbols", body))

	if add.Code != http.StatusCreated || !strings.Contains(add.Body.String(), `"symbol":"IBM"`) {
		t.Fatalf("expected created symbol, got %d: %s", add.Code, add.Body.String())
	}

	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/symbols?q=ibm", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"symbol":"IBM"`) {
		t.Fatalf("expected added symbol in list, got %d: %s", list.Code, list.Body.String())
	}
}

func TestHistoricalBatch(t *testing.T) {
	server, _ := newTestServer()
	body := strings.NewReader(`{"symbols":["AAPL","MSFT"],"interval":"daily"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/prices/historical/batch", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected batch 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "AAPL") || !strings.Contains(recorder.Body.String(), "MSFT") {
		t.Fatalf("expected batch symbols, got %s", recorder.Body.String())
	}
}

func TestHistoricalPricesRejectsInvalidInterval(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/prices/historical?symbol=AAPL&interval=yearly", nil)
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func newTestServer() (http.Handler, *apiFakeProvider) {
	provider := &apiFakeProvider{}
	registry := marketdata.NewRegistry()
	registry.Register(provider)
	service := marketdata.NewService(registry, []string{"fake"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	version := marketdata.RuntimeVersion{ServiceName: "market-data-service", Version: "test", Commit: "test", BuildTime: "test"}
	server := NewServer(service, []byte("openapi: 3.0.3\npaths:\n  /v1/prices/historical: {}\n"), version, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return server.Routes(), provider
}

func TestVersion(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "market-data-service") {
		t.Fatalf("expected version response, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenAPIContainsHistoricalPath(t *testing.T) {
	server, _ := newTestServer()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/swagger/openapi.yaml", nil))
	if !strings.Contains(recorder.Body.String(), "/v1/prices/historical") {
		t.Fatalf("expected historical path in openapi: %s", recorder.Body.String())
	}
}
