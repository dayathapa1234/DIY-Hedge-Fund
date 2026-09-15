# market-data-service

HTTP microservice for historical daily market prices with ordered provider fallback.

OpenAPI is the API contract for this service. Swagger UI uses it to render interactive docs, and future clients can use it to generate typed SDKs. The OpenAPI spec lives at `api/openapi.yaml` and is served at `/swagger/openapi.yaml`.

This service follows the standard documented in `docs/microservice-standard.md` so future Go services can use the same structure and operational behavior.

## What It Does

- Serves historical stock price data over HTTP.
- Supports provider priority and automatic fallback.
- Supports date ranges with inclusive `from` and `to` query parameters.
- Supports configurable price intervals such as daily, weekly, monthly, and hourly.
- Persists only symbols that are requested by users.
- Runs a feed updater for watched/searched symbols only.
- Exposes provider status, config, latest price, batch fetch, symbol search, and metrics endpoints.
- Ships with Swagger/OpenAPI documentation.
- Runs as Docker services with PostgreSQL and Redis.
- Uses graceful shutdown on `SIGINT` and `SIGTERM`.
- Uses request IDs, structured logs, panic recovery, security headers, and HTTP timeouts.
- Supports stdout logging, file logging, or both.
- Uses structured API errors with `code`, `message`, and `requestId`.
- Uses a lightweight provider circuit breaker and yfinance intraday range guardrails.
- Runs as a non-root container user with Docker healthchecks.

## Providers

- `stooq`: native Go CSV downloader using `https://stooq.com/q/d/l/?s=aapl.us&i=d`.
- `yfinance`: Python `yfinance` fallback executed inside the Docker image.
- `openbb`: optional HTTP integration. Set `OPENBB_BASE_URL` if you run an OpenBB-compatible API.
- `ghostfolio`: not used as a runtime provider. It is only useful as architecture inspiration for portfolio modeling.

Provider interval support:

| Provider | Supported intervals |
| --- | --- |
| `stooq` | `1d`, `1wk`, `1mo` |
| `yfinance` | `1m`, `2m`, `5m`, `15m`, `30m`, `60m`, `90m`, `1h`, `1d`, `5d`, `1wk`, `1mo`, `3mo` |
| `openbb` | receives the normalized interval and depends on your OpenBB API configuration |

Default priority:

```text
stooq,yfinance,openbb
```

## Run

```bash
docker compose up --build
```

The service listens on:

```text
http://localhost:8080
```

API contract:

```text
api/openapi.yaml
```

## API

Liveness and readiness:

```bash
curl "http://localhost:8080/livez"
curl "http://localhost:8080/readyz"
```

Version metadata:

```bash
curl "http://localhost:8080/version"
```

Provider status:

```bash
curl "http://localhost:8080/v1/providers"
```

Runtime config:

```bash
curl "http://localhost:8080/v1/config"
```

Fetch with default fallback priority:

```bash
curl "http://localhost:8080/v1/prices/historical?symbol=AAPL"
```

Fetch a date range:

```bash
curl "http://localhost:8080/v1/prices/historical?symbol=AAPL&from=2024-01-01&to=2024-12-31"
```

Force one provider:

```bash
curl "http://localhost:8080/v1/prices/historical?symbol=AAPL&provider=yfinance"
```

Override provider priority per request:

```bash
curl "http://localhost:8080/v1/prices/historical?symbol=AAPL&providers=yfinance,stooq,openbb"
```

Latest price:

```bash
curl "http://localhost:8080/v1/prices/latest?symbol=AAPL&interval=1d"
```

Batch historical prices:

```bash
curl -X POST "http://localhost:8080/v1/prices/historical/batch" \
  -H "Content-Type: application/json" \
  -d '{"symbols":["AAPL","MSFT"],"interval":"1d","from":"2024-01-01","to":"2024-01-31"}'
```

Symbol search:

```bash
curl "http://localhost:8080/v1/symbols/search?q=apple"
```

List symbols alphabetically with pagination:

```bash
curl "http://localhost:8080/v1/symbols?sort=symbol&order=asc&limit=50&offset=0"
```

List symbols with search, filters, and sorting:

```bash
curl "http://localhost:8080/v1/symbols?q=apple&exchange=NASDAQ&currency=USD&sort=name&order=asc&limit=20&offset=0"
```

Add a symbol to the runtime-local catalog:

```bash
curl -X POST "http://localhost:8080/v1/symbols" \
  -H "Content-Type: application/json" \
  -d '{"symbol":"IBM","name":"International Business Machines","exchange":"NYSE","currency":"USD"}'
```

When Postgres storage is enabled, added symbols are also marked as watched with daily interval (`1d`) so the feed refresh job updates their price data.

Prometheus-style metrics:

```bash
curl "http://localhost:8080/metrics"
```

File logs are written to:

```text
logs/market-data-service.log
```

Docker mounts the local `logs` folder into the container at `/app/logs`.

Date ranges use inclusive `YYYY-MM-DD` values. You can use only `from`, only `to`, or both.

Fetch weekly prices:

```bash
curl "http://localhost:8080/v1/prices/historical?symbol=AAPL&interval=1wk"
```

Fetch hourly prices with yfinance first:

```bash
curl "http://localhost:8080/v1/prices/historical?symbol=AAPL&interval=hourly&providers=yfinance,stooq"
```

Supported interval values:

```text
1m, 2m, 5m, 15m, 30m, 60m, 90m, 1h, 1d, 5d, 1wk, 1mo, 3mo
```

Accepted aliases:

```text
minute, hour, hourly, day, daily, week, weekly, month, monthly, quarter, quarterly
```

## Swagger

Swagger UI:

```text
http://localhost:8080/swagger
```

OpenAPI YAML:

```text
http://localhost:8080/swagger/openapi.yaml
```

## Environment

| Name | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address |
| `SERVICE_NAME` | `market-data-service` | Service name used in runtime config |
| `ENVIRONMENT` | `local` | Runtime environment name |
| `VERSION` | `dev` | Version reported by `/version` |
| `COMMIT_SHA` | `unknown` | Commit reported by `/version` |
| `BUILD_TIME` | `unknown` | Build time reported by `/version` |
| `LOG_OUTPUT` | `both` | `stdout`, `file`, or `both` |
| `LOG_FILE_PATH` | `logs/market-data-service.log` | JSON log file path when file logging is enabled |
| `READ_TIMEOUT` | `10s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `60s` | HTTP write timeout |
| `IDLE_TIMEOUT` | `120s` | HTTP idle timeout |
| `SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown timeout |
| `PROVIDER_TIMEOUT` | `30s` | Per-provider fetch timeout |
| `DEPENDENCY_RETRY` | `30s` | Startup retry budget for Postgres and Redis |
| `PROVIDER_PRIORITY` | `stooq,yfinance,openbb` | Ordered fallback list |
| `STOOQ_BASE_URL` | `https://stooq.com/q/d/l/` | Stooq CSV endpoint |
| `YFINANCE_PYTHON` | `python3` | Python executable for yfinance |
| `YFINANCE_SCRIPT` | `/app/scripts/yfinance_fetch.py` | yfinance bridge script |
| `OPENBB_BASE_URL` | empty | Optional OpenBB API base URL |
| `DATABASE_URL` | empty | PostgreSQL connection string. Persistence is disabled when empty |
| `REDIS_ADDR` | empty | Redis address. Falls back to in-memory cache when empty or unavailable |
| `REDIS_PASSWORD` | empty | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `CACHE_TTL` | `5m` | Response cache TTL for Redis or memory fallback |
| `FEED_ENABLED` | `true` with DB | Refresh watched symbols in the background |
| `FEED_INTERVAL` | `12h` | Watched-symbol refresh interval |
| `RATE_LIMIT_PER_MINUTE` | `120` | Per-client in-memory rate limit |

## Test

```bash
make test
```

Common development commands:

```bash
make fmt
make tidy
make build
make docker-up
make docker-down
```

Apply database migrations explicitly:

```bash
docker compose run --rm migrate
```

Run integration tests when Postgres and Redis are available:

```bash
TEST_DATABASE_URL="postgres://market:market@localhost:5432/market_data?sslmode=disable" TEST_REDIS_ADDR="localhost:6379" make test-integration
```

Database schema is documented in:

```text
migrations/001_init.sql
```

## Microservice Role

This service should be called by your finance app backend or a data-ingestion worker. When `DATABASE_URL` is configured, it stores only symbols that users requested. It does not pre-load the whole market.

Cache behavior:

```text
Redis -> PostgreSQL -> external providers
```

If `REDIS_ADDR` is not configured or Redis is unavailable, the service automatically uses in-memory cache instead.

Persistence tables:

```text
watched_symbols: symbol, interval, provider, first_requested_at, last_requested_at, last_refreshed_at
price_bars: symbol, interval, ts, provider, open, high, low, close, adj_close, volume
```

The `price_bars` primary key is:

```text
symbol, interval, ts, provider
```

Feed behavior:

```text
User requests AAPL daily -> AAPL/1d is saved in watched_symbols -> feed refreshes AAPL/1d on schedule
```
