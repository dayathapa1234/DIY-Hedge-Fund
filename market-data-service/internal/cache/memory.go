package cache

import (
	"context"
	"market-data-service/internal/marketdata"
	"sync"
	"time"
)

type Memory struct {
	ttl   time.Duration
	now   func() time.Time
	mu    sync.RWMutex
	items map[string]item
}

func (m *Memory) Ping(context.Context) error {
	return nil
}

func (m *Memory) Close() error {
	return nil
}

type item struct {
	result    marketdata.HistoricalResult
	expiresAt time.Time
}

func NewMemory(ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Memory{ttl: ttl, now: time.Now, items: map[string]item{}}
}

func (m *Memory) Get(query marketdata.HistoricalQuery) (marketdata.HistoricalResult, bool) {
	m.mu.RLock()
	item, ok := m.items[Key(query)]
	m.mu.RUnlock()
	if !ok || m.now().After(item.expiresAt) {
		return marketdata.HistoricalResult{}, false
	}
	return item.result, true
}

func (m *Memory) Set(query marketdata.HistoricalQuery, result marketdata.HistoricalResult) {
	m.mu.Lock()
	m.items[Key(query)] = item{result: result, expiresAt: m.now().Add(m.ttl)}
	m.mu.Unlock()
}
