package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"market-data-service/internal/marketdata"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OpenBB struct {
	baseURL string
	client  *http.Client
}

func NewOpenBB(baseURL string, timeout time.Duration) *OpenBB {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenBB{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout}}
}

func (o *OpenBB) Name() string {
	return "openbb"
}

func (o *OpenBB) Historical(ctx context.Context, query marketdata.HistoricalQuery) (marketdata.HistoricalResult, error) {
	if o.baseURL == "" {
		return marketdata.HistoricalResult{}, fmt.Errorf("OPENBB_BASE_URL is not configured")
	}

	endpoint, err := url.Parse(o.baseURL + "/api/v1/equity/price/historical")
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	params := endpoint.Query()
	params.Set("symbol", strings.ToUpper(query.Symbol))
	params.Set("interval", query.Interval)
	if !query.From.IsZero() {
		params.Set("start_date", query.From.Format("2006-01-02"))
	}
	if !query.To.IsZero() {
		params.Set("end_date", query.To.Format("2006-01-02"))
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return marketdata.HistoricalResult{}, fmt.Errorf("openbb returned status %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			Date     string  `json:"date"`
			Open     float64 `json:"open"`
			High     float64 `json:"high"`
			Low      float64 `json:"low"`
			Close    float64 `json:"close"`
			AdjClose float64 `json:"adj_close"`
			Volume   int64   `json:"volume"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return marketdata.HistoricalResult{}, err
	}

	prices := make([]marketdata.PriceBar, 0, len(payload.Results))
	for _, item := range payload.Results {
		date, err := time.Parse("2006-01-02", item.Date)
		if err != nil {
			continue
		}
		prices = append(prices, marketdata.PriceBar{Date: date, Open: item.Open, High: item.High, Low: item.Low, Close: item.Close, AdjClose: item.AdjClose, Volume: item.Volume})
	}
	if len(prices) == 0 {
		return marketdata.HistoricalResult{}, marketdata.ErrNoData
	}

	return marketdata.HistoricalResult{Symbol: query.Symbol, Provider: o.Name(), Interval: query.Interval, Prices: prices}, nil
}
