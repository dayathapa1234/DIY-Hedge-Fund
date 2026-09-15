package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"market-data-service/internal/marketdata"
	"os/exec"
	"strings"
	"time"
)

type YFinance struct {
	pythonPath string
	scriptPath string
	timeout    time.Duration
}

func NewYFinance(pythonPath, scriptPath string, timeout time.Duration) *YFinance {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &YFinance{pythonPath: pythonPath, scriptPath: scriptPath, timeout: timeout}
}

func (y *YFinance) Name() string {
	return "yfinance"
}

func (y *YFinance) Historical(ctx context.Context, query marketdata.HistoricalQuery) (marketdata.HistoricalResult, error) {
	ctx, cancel := context.WithTimeout(ctx, y.timeout)
	defer cancel()

	args := []string{y.scriptPath, strings.ToUpper(query.Symbol)}
	args = append(args, formatDateArg(query.From), formatDateArg(query.To))
	args = append(args, query.Interval)
	cmd := exec.CommandContext(ctx, y.pythonPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return marketdata.HistoricalResult{}, fmt.Errorf("yfinance failed: %s", message)
	}

	var result marketdata.HistoricalResult
	if err := json.Unmarshal(output, &result); err != nil {
		return marketdata.HistoricalResult{}, err
	}
	if len(result.Prices) == 0 {
		return marketdata.HistoricalResult{}, marketdata.ErrNoData
	}

	return result, nil
}

func formatDateArg(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}
