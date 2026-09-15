package marketdata

import "time"

type HistoricalQuery struct {
	Symbol   string
	Provider string
	Priority []string
	From     time.Time
	To       time.Time
	Interval string
	Refresh  bool
}

type PriceBar struct {
	Date     time.Time `json:"date"`
	Open     float64   `json:"open"`
	High     float64   `json:"high"`
	Low      float64   `json:"low"`
	Close    float64   `json:"close"`
	AdjClose float64   `json:"adjClose,omitempty"`
	Volume   int64     `json:"volume,omitempty"`
}

type HistoricalResult struct {
	Symbol          string      `json:"symbol"`
	RequestedSymbol string      `json:"requestedSymbol"`
	ResolvedSymbol  string      `json:"resolvedSymbol"`
	Provider        string      `json:"provider"`
	Interval        string      `json:"interval"`
	Currency        string      `json:"currency,omitempty"`
	Exchange        string      `json:"exchange,omitempty"`
	Timezone        string      `json:"timezone,omitempty"`
	From            *time.Time  `json:"from,omitempty"`
	To              *time.Time  `json:"to,omitempty"`
	Source          string      `json:"source"`
	CacheHit        bool        `json:"cacheHit"`
	Persisted       bool        `json:"persisted"`
	Stale           bool        `json:"stale"`
	DataQuality     DataQuality `json:"dataQuality"`
	Prices          []PriceBar  `json:"prices"`
}

type DataQuality struct {
	MissingDates      int  `json:"missingDates"`
	ZeroVolumeBars    int  `json:"zeroVolumeBars"`
	PotentiallyStale  bool `json:"potentiallyStale"`
	UnsupportedPeriod bool `json:"unsupportedPeriod"`
}

type ProviderStatus struct {
	Name               string    `json:"name"`
	Enabled            bool      `json:"enabled"`
	SupportedIntervals []string  `json:"supportedIntervals"`
	LastFailure        string    `json:"lastFailure,omitempty"`
	LastFailureAt      time.Time `json:"lastFailureAt,omitempty"`
}

type Config struct {
	ProviderPriority []string      `json:"providerPriority"`
	CacheBackend     string        `json:"cacheBackend"`
	CacheTTL         time.Duration `json:"cacheTtl"`
	Persistence      bool          `json:"persistence"`
	FeedEnabled      bool          `json:"feedEnabled"`
	FeedInterval     time.Duration `json:"feedInterval"`
	RateLimitPerMin  int           `json:"rateLimitPerMinute"`
}

type RuntimeVersion struct {
	ServiceName string `json:"serviceName"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	BuildTime   string `json:"buildTime"`
}

type MetricsSnapshot struct {
	CacheHits        map[string]int64 `json:"cacheHits"`
	ProviderRequests map[string]int64 `json:"providerRequests"`
	ProviderFailures map[string]int64 `json:"providerFailures"`
}

type SymbolSearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name,omitempty"`
	Exchange string `json:"exchange,omitempty"`
	Currency string `json:"currency,omitempty"`
	Provider string `json:"provider"`
}

type SymbolListQuery struct {
	Text     string
	Exchange string
	Currency string
	Provider string
	Sort     string
	Order    string
	Limit    int
	Offset   int
}

type SymbolListResult struct {
	Results []SymbolSearchResult `json:"results"`
	Total   int                  `json:"total"`
	Limit   int                  `json:"limit"`
	Offset  int                  `json:"offset"`
}
