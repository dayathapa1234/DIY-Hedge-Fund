package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"market-data-service/internal/marketdata"
)

type Server struct {
	service *marketdata.Service
	openAPI []byte
	version marketdata.RuntimeVersion
	logger  *slog.Logger
	metrics metrics
	limiter *rateLimiter
}

type metrics struct {
	Requests       atomic.Int64
	ProviderErrors atomic.Int64
	RateLimited    atomic.Int64
}

func NewServer(service *marketdata.Service, openAPI []byte, version marketdata.RuntimeVersion, logger *slog.Logger) *Server {
	return &Server{service: service, openAPI: openAPI, version: version, logger: logger, limiter: newRateLimiter(service.Config().RateLimitPerMin)}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /swagger", s.handleSwaggerUI)
	mux.HandleFunc("GET /swagger/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("GET /v1/prices/historical", s.handleHistoricalPrices)
	mux.HandleFunc("POST /v1/prices/historical/batch", s.handleHistoricalBatch)
	mux.HandleFunc("GET /v1/prices/latest", s.handleLatestPrice)
	mux.HandleFunc("GET /v1/providers", s.handleProviders)
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("GET /version", s.handleVersion)
	mux.HandleFunc("GET /v1/symbols", s.handleSymbols)
	mux.HandleFunc("POST /v1/symbols", s.handleAddSymbol)
	mux.HandleFunc("GET /v1/symbols/search", s.handleSymbolSearch)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	return s.recoverMiddleware(s.securityHeadersMiddleware(s.requestIDMiddleware(s.loggingMiddleware(mux))))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	checks := s.service.Ready(r.Context())
	for _, status := range checks {
		if status != "ok" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "checks": checks})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "checks": checks})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if len(s.openAPI) == 0 {
		writeError(w, http.StatusInternalServerError, "OPENAPI_UNAVAILABLE", "openapi document is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.openAPI)
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerHTML))
}

func (s *Server) handleHistoricalPrices(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r) {
		return
	}
	query, ok := parseHistoricalQuery(w, r)
	if !ok {
		return
	}

	result, err := s.service.Historical(r.Context(), query)
	if err != nil {
		s.metrics.ProviderErrors.Add(1)
		status := http.StatusBadGateway
		if errors.Is(err, marketdata.ErrUnknownProvider) || errors.Is(err, marketdata.ErrNoProviders) || errors.Is(err, marketdata.ErrInvalidInterval) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "PROVIDER_FAILURE", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleHistoricalBatch(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r) {
		return
	}
	var request struct {
		Symbols   []string `json:"symbols"`
		Provider  string   `json:"provider"`
		Providers []string `json:"providers"`
		From      string   `json:"from"`
		To        string   `json:"to"`
		Interval  string   `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}
	if len(request.Symbols) == 0 {
		writeError(w, http.StatusBadRequest, "SYMBOLS_REQUIRED", "symbols is required")
		return
	}
	from, err := parseDateParam(request.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FROM_DATE", "from must use YYYY-MM-DD")
		return
	}
	to, err := parseDateParam(request.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TO_DATE", "to must use YYYY-MM-DD")
		return
	}
	interval, err := marketdata.NormalizeInterval(request.Interval)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INTERVAL", "invalid interval")
		return
	}

	responses := make(map[string]any, len(request.Symbols))
	for _, symbol := range request.Symbols {
		query := marketdata.HistoricalQuery{Symbol: symbol, Provider: request.Provider, Priority: request.Providers, From: from, To: to, Interval: interval}
		result, err := s.service.Historical(r.Context(), query)
		if err != nil {
			responses[symbol] = map[string]string{"error": err.Error()}
			continue
		}
		responses[symbol] = result
	}
	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) handleLatestPrice(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r) {
		return
	}
	query, ok := parseHistoricalQuery(w, r)
	if !ok {
		return
	}
	price, result, err := s.service.Latest(r.Context(), query)
	if err != nil {
		s.metrics.ProviderErrors.Add(1)
		writeError(w, http.StatusBadGateway, "PROVIDER_FAILURE", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"symbol": result.Symbol, "provider": result.Provider, "interval": result.Interval, "source": result.Source, "price": price})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"providers": s.service.Providers()})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.service.Config())
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.version)
}

func (s *Server) handleSymbolSearch(w http.ResponseWriter, r *http.Request) {
	results, err := s.service.SearchSymbols(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	query, ok := parseSymbolListQuery(w, r)
	if !ok {
		return
	}
	results, err := s.service.ListSymbols(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleAddSymbol(w http.ResponseWriter, r *http.Request) {
	var request marketdata.SymbolSearchResult
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}
	symbol, err := s.service.AddSymbol(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SYMBOL", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, symbol)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "market_data_requests_total %d\n", s.metrics.Requests.Load())
	_, _ = fmt.Fprintf(w, "market_data_provider_errors_total %d\n", s.metrics.ProviderErrors.Load())
	_, _ = fmt.Fprintf(w, "market_data_rate_limited_total %d\n", s.metrics.RateLimited.Load())
	snapshot := s.service.Metrics()
	for backend, count := range snapshot.CacheHits {
		_, _ = fmt.Fprintf(w, "market_data_cache_hits_total{backend=%q} %d\n", backend, count)
	}
	for provider, count := range snapshot.ProviderRequests {
		_, _ = fmt.Fprintf(w, "market_data_provider_requests_total{provider=%q} %d\n", provider, count)
	}
	for provider, count := range snapshot.ProviderFailures {
		_, _ = fmt.Fprintf(w, "market_data_provider_failures_total{provider=%q} %d\n", provider, count)
	}
}

func parseHistoricalQuery(w http.ResponseWriter, r *http.Request) (marketdata.HistoricalQuery, bool) {
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "SYMBOL_REQUIRED", "symbol is required")
		return marketdata.HistoricalQuery{}, false
	}

	from, err := parseDateParam(r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FROM_DATE", "from must use YYYY-MM-DD")
		return marketdata.HistoricalQuery{}, false
	}
	to, err := parseDateParam(r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TO_DATE", "to must use YYYY-MM-DD")
		return marketdata.HistoricalQuery{}, false
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		writeError(w, http.StatusBadRequest, "INVALID_DATE_RANGE", "from must be before or equal to to")
		return marketdata.HistoricalQuery{}, false
	}
	interval, err := marketdata.NormalizeInterval(r.URL.Query().Get("interval"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INTERVAL", "interval must be one of 1m,2m,5m,15m,30m,60m,90m,1h,1d,5d,1wk,1mo,3mo or aliases like hourly,daily,weekly,monthly")
		return marketdata.HistoricalQuery{}, false
	}

	query := marketdata.HistoricalQuery{
		Symbol:   symbol,
		Provider: strings.TrimSpace(r.URL.Query().Get("provider")),
		From:     from,
		To:       to,
		Interval: interval,
	}
	if providersParam := strings.TrimSpace(r.URL.Query().Get("providers")); providersParam != "" {
		query.Priority = splitCSV(providersParam)
	}
	return query, true
}

func parseSymbolListQuery(w http.ResponseWriter, r *http.Request) (marketdata.SymbolListQuery, bool) {
	values := r.URL.Query()
	limit, ok := parseOptionalInt(w, values.Get("limit"), "limit")
	if !ok {
		return marketdata.SymbolListQuery{}, false
	}
	offset, ok := parseOptionalInt(w, values.Get("offset"), "offset")
	if !ok {
		return marketdata.SymbolListQuery{}, false
	}
	return marketdata.SymbolListQuery{
		Text:     values.Get("q"),
		Exchange: values.Get("exchange"),
		Currency: values.Get("currency"),
		Provider: values.Get("provider"),
		Sort:     values.Get("sort"),
		Order:    values.Get("order"),
		Limit:    limit,
		Offset:   offset,
	}, true
}

func parseOptionalInt(w http.ResponseWriter, value string, name string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", name+" must be an integer")
		return 0, false
	}
	return parsed, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message, "requestId": requestIDFromHeader(w)})
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func parseDateParam(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", value)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.metrics.Requests.Add(1)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", recorder.status, "request_id", r.Header.Get("X-Request-Id"), "duration_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "panic", recovered, "request_id", r.Header.Get("X-Request-Id"))
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = fmt.Sprintf("%d", time.Now().UnixNano())
			r.Header.Set("X-Request-Id", requestID)
		}
		w.Header().Set("X-Request-Id", requestID)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestIDFromHeader(w http.ResponseWriter) string {
	return w.Header().Get("X-Request-Id")
}

func (s *Server) allow(w http.ResponseWriter, r *http.Request) bool {
	if s.limiter == nil || s.limiter.allow(r.RemoteAddr) {
		return true
	}
	s.metrics.RateLimited.Add(1)
	writeError(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "rate limit exceeded")
	return false
}

type rateLimiter struct {
	limit int
	mu    sync.Mutex
	items map[string]bucket
}

type bucket struct {
	window time.Time
	count  int
}

func newRateLimiter(limit int) *rateLimiter {
	if limit <= 0 {
		return nil
	}
	return &rateLimiter{limit: limit, items: map[string]bucket{}}
}

func (r *rateLimiter) allow(key string) bool {
	window := time.Now().UTC().Truncate(time.Minute)
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.items[key]
	if !b.window.Equal(window) {
		b = bucket{window: window}
	}
	b.count++
	r.items[key] = b
	return b.count <= r.limit
}

const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Market Data Service API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: '/swagger/openapi.yaml', dom_id: '#swagger-ui' });
  </script>
</body>
</html>`
