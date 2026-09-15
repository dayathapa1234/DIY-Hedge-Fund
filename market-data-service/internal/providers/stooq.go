package providers

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"market-data-service/internal/marketdata"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Stooq struct {
	baseURL string
	client  *http.Client
}

func NewStooq(baseURL string, timeout time.Duration) *Stooq {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Stooq{baseURL: baseURL, client: &http.Client{Timeout: timeout}}
}

func (s *Stooq) Name() string {
	return "stooq"
}

func (s *Stooq) Historical(ctx context.Context, query marketdata.HistoricalQuery) (marketdata.HistoricalResult, error) {
	symbol := stooqSymbol(query.Symbol)
	interval, err := stooqInterval(query.Interval)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	endpoint, err := url.Parse(s.baseURL)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	params := endpoint.Query()
	params.Set("s", symbol)
	params.Set("i", interval)
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	req.Header.Set("User-Agent", "market-data-service/0.1")

	resp, err := s.client.Do(req)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return marketdata.HistoricalResult{}, fmt.Errorf("stooq returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	trimmedBody := strings.TrimSpace(string(body))
	if strings.HasPrefix(strings.ToLower(trimmedBody), "<!doctype html") || strings.Contains(strings.ToLower(trimmedBody), "requires javascript") {
		return marketdata.HistoricalResult{}, fmt.Errorf("stooq returned browser verification page")
	}

	reader := csv.NewReader(bytes.NewReader(body))
	header, err := reader.Read()
	if err != nil {
		return marketdata.HistoricalResult{}, err
	}
	if len(header) == 0 || strings.EqualFold(header[0], "No data") {
		return marketdata.HistoricalResult{}, marketdata.ErrNoData
	}

	var prices []marketdata.PriceBar
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return marketdata.HistoricalResult{}, err
		}
		if len(record) < 6 {
			continue
		}

		bar, err := parseStooqRecord(record)
		if err != nil {
			continue
		}
		prices = append(prices, bar)
	}

	if len(prices) == 0 {
		return marketdata.HistoricalResult{}, marketdata.ErrNoData
	}

	return marketdata.HistoricalResult{Symbol: query.Symbol, Provider: s.Name(), Interval: query.Interval, Prices: prices}, nil
}

func stooqInterval(interval string) (string, error) {
	switch interval {
	case "", "1d":
		return "d", nil
	case "1wk":
		return "w", nil
	case "1mo":
		return "m", nil
	default:
		return "", fmt.Errorf("stooq does not support interval %s", interval)
	}
}

func stooqSymbol(symbol string) string {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if strings.Contains(symbol, ".") {
		return symbol
	}
	return symbol + ".us"
}

func parseStooqRecord(record []string) (marketdata.PriceBar, error) {
	date, err := time.Parse("2006-01-02", record[0])
	if err != nil {
		return marketdata.PriceBar{}, err
	}
	open, err := strconv.ParseFloat(record[1], 64)
	if err != nil {
		return marketdata.PriceBar{}, err
	}
	high, err := strconv.ParseFloat(record[2], 64)
	if err != nil {
		return marketdata.PriceBar{}, err
	}
	low, err := strconv.ParseFloat(record[3], 64)
	if err != nil {
		return marketdata.PriceBar{}, err
	}
	closePrice, err := strconv.ParseFloat(record[4], 64)
	if err != nil {
		return marketdata.PriceBar{}, err
	}
	volume, _ := strconv.ParseInt(record[5], 10, 64)

	return marketdata.PriceBar{Date: date, Open: open, High: high, Low: low, Close: closePrice, AdjClose: closePrice, Volume: volume}, nil
}
