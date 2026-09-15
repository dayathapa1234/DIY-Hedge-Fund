package feed

import (
	"context"
	"log/slog"
	"market-data-service/internal/marketdata"
	"time"
)

type Refresher struct {
	service  *marketdata.Service
	interval time.Duration
	logger   *slog.Logger
}

func NewRefresher(service *marketdata.Service, interval time.Duration, logger *slog.Logger) *Refresher {
	if interval <= 0 {
		interval = 12 * time.Hour
	}
	return &Refresher{service: service, interval: interval, logger: logger}
}

func (r *Refresher) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshCtx, cancel := context.WithTimeout(ctx, r.interval)
				if err := r.service.RefreshWatched(refreshCtx); err != nil {
					r.logger.Warn("feed refresh failed", "error", err)
				}
				cancel()
			}
		}
	}()
}
