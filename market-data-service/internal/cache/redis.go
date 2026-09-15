package cache

import (
	"context"
	"encoding/json"
	"market-data-service/internal/marketdata"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedis(addr string, password string, db int, ttl time.Duration) *Redis {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Redis{
		client: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}),
		ttl:    ttl,
	}
}

func (r *Redis) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *Redis) Close() error {
	return r.client.Close()
}

func (r *Redis) Get(query marketdata.HistoricalQuery) (marketdata.HistoricalResult, bool) {
	value, err := r.client.Get(context.Background(), Key(query)).Result()
	if err != nil {
		return marketdata.HistoricalResult{}, false
	}

	var result marketdata.HistoricalResult
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return marketdata.HistoricalResult{}, false
	}
	return result, true
}

func (r *Redis) Set(query marketdata.HistoricalQuery, result marketdata.HistoricalResult) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return
	}
	_ = r.client.Set(context.Background(), Key(query), encoded, r.ttl).Err()
}
