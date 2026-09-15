package marketdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoData          = errors.New("provider returned no data")
	ErrNoProviders     = errors.New("no providers configured")
	ErrUnknownProvider = errors.New("unknown provider")
	ErrInvalidInterval = errors.New("invalid interval")
	ErrCircuitOpen     = errors.New("provider circuit is open")
	ErrQueryTooLarge   = errors.New("query exceeds provider limits")
)

type Provider interface {
	Name() string
	Historical(context.Context, HistoricalQuery) (HistoricalResult, error)
}

type Store interface {
	Migrate(context.Context) error
	Ping(context.Context) error
	Close()
	GetPrices(context.Context, HistoricalQuery) (HistoricalResult, error)
	UpsertPrices(context.Context, HistoricalResult) error
	MarkWatched(context.Context, HistoricalQuery) error
	ListWatched(context.Context) ([]HistoricalQuery, error)
}

type Cache interface {
	Ping(context.Context) error
	Close() error
	Get(HistoricalQuery) (HistoricalResult, bool)
	Set(HistoricalQuery, HistoricalResult)
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

func (r *Registry) Register(provider Provider) {
	r.providers[strings.ToLower(provider.Name())] = provider
}

func (r *Registry) Get(name string) (Provider, bool) {
	provider, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	return provider, ok
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

type Service struct {
	registry *Registry
	priority []string
	logger   *slog.Logger
	store    Store
	cache    Cache
	status   map[string]ProviderStatus
	failures map[string]int
	symbols  map[string]SymbolSearchResult
	mu       sync.Mutex
	config   Config
	metrics  MetricsSnapshot
}

func NewService(registry *Registry, priority []string, logger *slog.Logger) *Service {
	return &Service{
		registry: registry,
		priority: priority,
		logger:   logger,
		status:   map[string]ProviderStatus{},
		failures: map[string]int{},
		symbols:  map[string]SymbolSearchResult{},
		metrics:  MetricsSnapshot{CacheHits: map[string]int64{}, ProviderRequests: map[string]int64{}, ProviderFailures: map[string]int64{}},
		config:   Config{ProviderPriority: priority, CacheBackend: "none", CacheTTL: 5 * time.Minute, RateLimitPerMin: 120},
	}
}

func (s *Service) Close() error {
	if s.cache != nil {
		_ = s.cache.Close()
	}
	if s.store != nil {
		s.store.Close()
	}
	return nil
}

func (s *Service) SetStore(store Store) {
	s.store = store
	s.config.Persistence = store != nil
}

func (s *Service) SetCache(cache Cache, backend string, ttl time.Duration) {
	s.cache = cache
	s.config.CacheBackend = backend
	if ttl > 0 {
		s.config.CacheTTL = ttl
	}
}

func (s *Service) SetFeedConfig(enabled bool, interval time.Duration) {
	s.config.FeedEnabled = enabled
	s.config.FeedInterval = interval
}

func (s *Service) SetRateLimit(limit int) {
	s.config.RateLimitPerMin = limit
}

func (s *Service) Config() Config {
	return s.config
}

func (s *Service) Metrics() MetricsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return MetricsSnapshot{CacheHits: cloneInt64Map(s.metrics.CacheHits), ProviderRequests: cloneInt64Map(s.metrics.ProviderRequests), ProviderFailures: cloneInt64Map(s.metrics.ProviderFailures)}
}

func (s *Service) Ready(ctx context.Context) map[string]string {
	checks := map[string]string{"service": "ok"}
	if s.store != nil {
		if err := s.store.Ping(ctx); err != nil {
			checks["postgres"] = err.Error()
		} else {
			checks["postgres"] = "ok"
		}
	}
	if s.cache != nil {
		if err := s.cache.Ping(ctx); err != nil {
			checks["cache"] = err.Error()
		} else {
			checks["cache"] = "ok"
		}
	}
	return checks
}

func (s *Service) Providers() []ProviderStatus {
	statuses := make([]ProviderStatus, 0, len(s.registry.providers))
	for _, name := range s.registry.Names() {
		status := s.status[name]
		status.Name = name
		status.Enabled = true
		status.SupportedIntervals = supportedIntervals(name)
		statuses = append(statuses, status)
	}
	return statuses
}

func (s *Service) Historical(ctx context.Context, query HistoricalQuery) (HistoricalResult, error) {
	requestedSymbol := strings.ToUpper(strings.TrimSpace(query.Symbol))
	query.Symbol = strings.ToUpper(strings.TrimSpace(query.Symbol))
	if query.Symbol == "" {
		return HistoricalResult{}, fmt.Errorf("symbol is required")
	}
	interval, err := NormalizeInterval(query.Interval)
	if err != nil {
		return HistoricalResult{}, err
	}
	query.Interval = interval

	priority := s.priority
	if len(query.Priority) > 0 {
		priority = query.Priority
	}
	if strings.TrimSpace(query.Provider) != "" {
		priority = []string{query.Provider}
	}
	if len(priority) == 0 {
		return HistoricalResult{}, ErrNoProviders
	}
	if s.cache != nil && !query.Refresh {
		if result, ok := s.cache.Get(query); ok {
			s.increment(s.metrics.CacheHits, s.config.CacheBackend)
			result.Source = "cache"
			result.CacheHit = true
			return result, nil
		}
	}
	if s.store != nil && !query.Refresh {
		result, err := s.store.GetPrices(ctx, query)
		if err == nil && len(result.Prices) > 0 {
			result = s.enrichResult(query, result, requestedSymbol, result.Provider, "database")
			result.Persisted = true
			if s.cache != nil {
				s.cache.Set(query, result)
			}
			return result, nil
		}
	}

	var failures []string
	for _, providerName := range priority {
		provider, ok := s.registry.Get(providerName)
		if !ok {
			if len(priority) == 1 {
				return HistoricalResult{}, fmt.Errorf("%w: %s", ErrUnknownProvider, providerName)
			}
			failures = append(failures, providerName+": unknown provider")
			continue
		}
		if err := s.providerAvailable(provider.Name()); err != nil {
			failures = append(failures, provider.Name()+": "+err.Error())
			continue
		}
		if err := validateProviderLimits(provider.Name(), query); err != nil {
			failures = append(failures, provider.Name()+": "+err.Error())
			continue
		}
		s.increment(s.metrics.ProviderRequests, provider.Name())

		result, err := provider.Historical(ctx, query)
		if err == nil && len(result.Prices) > 0 {
			result.Prices = filterPrices(result.Prices, query.From, query.To)
		}
		if err == nil && len(result.Prices) > 0 {
			result = s.enrichResult(query, result, requestedSymbol, provider.Name(), "provider")
			if s.store != nil {
				if err := s.store.MarkWatched(ctx, query); err != nil {
					s.logger.Warn("failed to mark symbol watched", "symbol", query.Symbol, "interval", query.Interval, "error", err)
				}
				if err := s.store.UpsertPrices(ctx, result); err != nil {
					s.logger.Warn("failed to persist prices", "symbol", query.Symbol, "interval", query.Interval, "provider", provider.Name(), "error", err)
				} else {
					result.Persisted = true
				}
			}
			if s.cache != nil {
				s.cache.Set(query, result)
			}
			s.recordSuccess(provider.Name())
			return result, nil
		}

		if err == nil {
			err = ErrNoData
		}
		failures = append(failures, provider.Name()+": "+err.Error())
		s.recordFailure(provider.Name(), err)
		s.logger.Warn("provider failed", "provider", provider.Name(), "symbol", query.Symbol, "error", err)
	}

	return HistoricalResult{}, fmt.Errorf("all providers failed for %s: %s", query.Symbol, strings.Join(failures, "; "))
}

func (s *Service) Latest(ctx context.Context, query HistoricalQuery) (PriceBar, HistoricalResult, error) {
	result, err := s.Historical(ctx, query)
	if err != nil {
		return PriceBar{}, HistoricalResult{}, err
	}
	if len(result.Prices) == 0 {
		return PriceBar{}, HistoricalResult{}, ErrNoData
	}
	return result.Prices[len(result.Prices)-1], result, nil
}

func (s *Service) SearchSymbols(ctx context.Context, text string) ([]SymbolSearchResult, error) {
	text = strings.ToUpper(strings.TrimSpace(text))
	if text == "" {
		return nil, fmt.Errorf("q is required")
	}

	results := []SymbolSearchResult{{Symbol: text, Provider: "local"}}
	for _, item := range s.allSymbols() {
		if strings.Contains(item.Symbol, text) || strings.Contains(strings.ToUpper(item.Name), text) {
			results = append(results, item)
		}
	}
	return dedupeSymbols(results), nil
}

func (s *Service) ListSymbols(ctx context.Context, query SymbolListQuery) (SymbolListResult, error) {
	query.Text = strings.ToUpper(strings.TrimSpace(query.Text))
	query.Exchange = strings.ToUpper(strings.TrimSpace(query.Exchange))
	query.Currency = strings.ToUpper(strings.TrimSpace(query.Currency))
	query.Provider = strings.ToLower(strings.TrimSpace(query.Provider))
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	query.Order = strings.ToLower(strings.TrimSpace(query.Order))
	if query.Sort == "" {
		query.Sort = "symbol"
	}
	if query.Order == "" {
		query.Order = "asc"
	}
	if !validSymbolSort(query.Sort) {
		return SymbolListResult{}, fmt.Errorf("sort must be one of symbol, name, exchange, currency, provider")
	}
	if query.Order != "asc" && query.Order != "desc" {
		return SymbolListResult{}, fmt.Errorf("order must be asc or desc")
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.Offset < 0 {
		return SymbolListResult{}, fmt.Errorf("offset must be greater than or equal to 0")
	}

	symbols := s.allSymbols()
	filtered := make([]SymbolSearchResult, 0, len(symbols))
	for _, item := range symbols {
		if query.Text != "" && !strings.Contains(item.Symbol, query.Text) && !strings.Contains(strings.ToUpper(item.Name), query.Text) {
			continue
		}
		if query.Exchange != "" && strings.ToUpper(item.Exchange) != query.Exchange {
			continue
		}
		if query.Currency != "" && strings.ToUpper(item.Currency) != query.Currency {
			continue
		}
		if query.Provider != "" && strings.ToLower(item.Provider) != query.Provider {
			continue
		}
		filtered = append(filtered, item)
	}
	sortSymbols(filtered, query.Sort, query.Order)

	total := len(filtered)
	start := query.Offset
	if start > total {
		start = total
	}
	end := start + query.Limit
	if end > total {
		end = total
	}
	return SymbolListResult{Results: filtered[start:end], Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (s *Service) AddSymbol(ctx context.Context, symbol SymbolSearchResult) (SymbolSearchResult, error) {
	symbol.Symbol = strings.ToUpper(strings.TrimSpace(symbol.Symbol))
	symbol.Name = strings.TrimSpace(symbol.Name)
	symbol.Exchange = strings.ToUpper(strings.TrimSpace(symbol.Exchange))
	symbol.Currency = strings.ToUpper(strings.TrimSpace(symbol.Currency))
	symbol.Provider = strings.ToLower(strings.TrimSpace(symbol.Provider))
	if symbol.Symbol == "" {
		return SymbolSearchResult{}, fmt.Errorf("symbol is required")
	}
	if symbol.Provider == "" {
		symbol.Provider = "local"
	}
	if s.store != nil {
		query := HistoricalQuery{Symbol: symbol.Symbol, Interval: "1d"}
		if err := s.store.MarkWatched(ctx, query); err != nil {
			return SymbolSearchResult{}, err
		}
	}
	s.mu.Lock()
	s.symbols[symbol.Symbol] = symbol
	s.mu.Unlock()
	return symbol, nil
}

func (s *Service) allSymbols() []SymbolSearchResult {
	merged := map[string]SymbolSearchResult{}
	for _, item := range symbolCatalog() {
		merged[item.Symbol] = item
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for symbol, item := range s.symbols {
		merged[symbol] = item
	}
	items := make([]SymbolSearchResult, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	return items
}

func validSymbolSort(value string) bool {
	switch value {
	case "symbol", "name", "exchange", "currency", "provider":
		return true
	default:
		return false
	}
}

func sortSymbols(items []SymbolSearchResult, field string, order string) {
	sort.SliceStable(items, func(i, j int) bool {
		left := symbolSortValue(items[i], field)
		right := symbolSortValue(items[j], field)
		if left == right {
			left = items[i].Symbol
			right = items[j].Symbol
		}
		if order == "desc" {
			return left > right
		}
		return left < right
	})
}

func symbolSortValue(item SymbolSearchResult, field string) string {
	switch field {
	case "name":
		return strings.ToUpper(item.Name)
	case "exchange":
		return strings.ToUpper(item.Exchange)
	case "currency":
		return strings.ToUpper(item.Currency)
	case "provider":
		return strings.ToUpper(item.Provider)
	default:
		return item.Symbol
	}
}

func symbolCatalog() []SymbolSearchResult {
	return []SymbolSearchResult{
		{Symbol: "AAPL", Name: "Apple Inc.", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "MSFT", Name: "Microsoft Corporation", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "GOOGL", Name: "Alphabet Inc.", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "AMZN", Name: "Amazon.com Inc.", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "TSLA", Name: "Tesla Inc.", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "NVDA", Name: "NVIDIA Corporation", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "META", Name: "Meta Platforms Inc.", Exchange: "NASDAQ", Currency: "USD", Provider: "local"},
		{Symbol: "SPY", Name: "SPDR S&P 500 ETF Trust", Exchange: "NYSEARCA", Currency: "USD", Provider: "local"},
	}
}

func dedupeSymbols(results []SymbolSearchResult) []SymbolSearchResult {
	seen := map[string]bool{}
	deduped := make([]SymbolSearchResult, 0, len(results))
	for _, result := range results {
		if seen[result.Symbol] {
			continue
		}
		seen[result.Symbol] = true
		deduped = append(deduped, result)
	}
	return deduped
}

func (s *Service) RefreshWatched(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	queries, err := s.store.ListWatched(ctx)
	if err != nil {
		return err
	}
	for _, query := range queries {
		query.Refresh = true
		if _, err := s.Historical(ctx, query); err != nil {
			s.logger.Warn("watched symbol refresh failed", "symbol", query.Symbol, "interval", query.Interval, "error", err)
		}
	}
	return nil
}

func (s *Service) enrichResult(query HistoricalQuery, result HistoricalResult, requestedSymbol string, provider string, source string) HistoricalResult {
	result.Symbol = query.Symbol
	result.RequestedSymbol = requestedSymbol
	if result.ResolvedSymbol == "" {
		result.ResolvedSymbol = query.Symbol
	}
	result.Provider = provider
	result.Interval = query.Interval
	result.From = timePtr(query.From)
	result.To = timePtr(query.To)
	result.Source = source
	result.DataQuality = assessDataQuality(result.Prices, query.Interval)
	result.Stale = result.DataQuality.PotentiallyStale
	return result
}

func (s *Service) recordFailure(provider string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[provider]++
	s.metrics.ProviderFailures[provider]++
	status := s.status[provider]
	status.Name = provider
	status.Enabled = true
	status.SupportedIntervals = supportedIntervals(provider)
	status.LastFailure = err.Error()
	status.LastFailureAt = time.Now().UTC()
	s.status[provider] = status
}

func (s *Service) increment(values map[string]int64, key string) {
	s.mu.Lock()
	values[key]++
	s.mu.Unlock()
}

func cloneInt64Map(values map[string]int64) map[string]int64 {
	cloned := make(map[string]int64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (s *Service) recordSuccess(provider string) {
	s.mu.Lock()
	delete(s.failures, provider)
	status := s.status[provider]
	status.LastFailure = ""
	status.LastFailureAt = time.Time{}
	s.status[provider] = status
	s.mu.Unlock()
}

func (s *Service) providerAvailable(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status[provider]
	if s.failures[provider] >= 3 && time.Since(status.LastFailureAt) < time.Minute {
		return fmt.Errorf("%w: %s", ErrCircuitOpen, provider)
	}
	return nil
}

func validateProviderLimits(provider string, query HistoricalQuery) error {
	if provider != "yfinance" || !isIntraday(query.Interval) || query.From.IsZero() || query.To.IsZero() {
		return nil
	}
	if query.To.Sub(query.From) > 60*24*time.Hour {
		return fmt.Errorf("%w: yfinance intraday ranges are limited to 60 days", ErrQueryTooLarge)
	}
	return nil
}

func isIntraday(interval string) bool {
	switch interval {
	case "1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h":
		return true
	default:
		return false
	}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	date := dateOnly(value)
	return &date
}

func supportedIntervals(provider string) []string {
	switch provider {
	case "stooq":
		return []string{"1d", "1wk", "1mo"}
	case "yfinance":
		return []string{"1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "5d", "1wk", "1mo", "3mo"}
	case "openbb":
		return []string{"provider-dependent"}
	default:
		return nil
	}
}

func assessDataQuality(prices []PriceBar, interval string) DataQuality {
	quality := DataQuality{}
	if len(prices) == 0 {
		quality.PotentiallyStale = true
		return quality
	}
	for _, price := range prices {
		if price.Volume == 0 {
			quality.ZeroVolumeBars++
		}
	}
	latest := prices[len(prices)-1].Date
	if interval == "1d" && time.Since(latest) > 96*time.Hour {
		quality.PotentiallyStale = true
	}
	return quality
}

func filterPrices(prices []PriceBar, from time.Time, to time.Time) []PriceBar {
	if from.IsZero() && to.IsZero() {
		return prices
	}

	filtered := make([]PriceBar, 0, len(prices))
	for _, price := range prices {
		date := dateOnly(price.Date)
		if !from.IsZero() && date.Before(dateOnly(from)) {
			continue
		}
		if !to.IsZero() && date.After(dateOnly(to)) {
			continue
		}
		filtered = append(filtered, price)
	}
	return filtered
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func NormalizeInterval(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "1d", nil
	}

	aliases := map[string]string{
		"minute":    "1m",
		"minutes":   "1m",
		"hour":      "1h",
		"hours":     "1h",
		"hourly":    "1h",
		"day":       "1d",
		"days":      "1d",
		"daily":     "1d",
		"week":      "1wk",
		"weeks":     "1wk",
		"weekly":    "1wk",
		"month":     "1mo",
		"months":    "1mo",
		"monthly":   "1mo",
		"quarter":   "3mo",
		"quarters":  "3mo",
		"quarterly": "3mo",
	}
	if normalized, ok := aliases[value]; ok {
		return normalized, nil
	}

	supported := map[string]bool{
		"1m": true, "2m": true, "5m": true, "15m": true, "30m": true,
		"60m": true, "90m": true, "1h": true, "1d": true, "5d": true,
		"1wk": true, "1mo": true, "3mo": true,
	}
	if supported[value] {
		return value, nil
	}

	return "", fmt.Errorf("%w: %s", ErrInvalidInterval, value)
}
