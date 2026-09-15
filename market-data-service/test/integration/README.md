# Integration Tests

Planned integration tests should run against real Postgres and Redis containers.

Coverage targets:

- Redis cache hit after first successful provider response.
- PostgreSQL persistence into `price_bars`.
- Requested symbols tracked in `watched_symbols`.
- Feed refresh updates only watched symbols.
- `/readyz` returns `503` when a configured dependency is unavailable.

Keep unit tests fast under `go test ./...`. Run Docker-backed integration tests explicitly:

```bash
TEST_DATABASE_URL="postgres://market:market@localhost:5432/market_data?sslmode=disable" TEST_REDIS_ADDR="localhost:6379" make test-integration
```
