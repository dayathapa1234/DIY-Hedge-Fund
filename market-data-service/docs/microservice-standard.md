# Go Microservice Standard

Use this as the baseline for other services in this project.

## Layout

- `cmd/<service-name>` contains only bootstrap code.
- `internal/config` owns environment parsing and validation.
- `internal/httpapi` owns HTTP handlers, middleware, and transport-level tests.
- Domain packages stay under `internal/<domain>`.
- Integrations stay under explicit packages such as `internal/storage`, `internal/cache`, and `internal/providers`.
- API documentation is embedded and served at `/swagger` and `/swagger/openapi.yaml`.

## Runtime

- Configuration comes from environment variables.
- The process handles `SIGINT` and `SIGTERM` gracefully.
- HTTP server has read, write, idle, and shutdown timeouts.
- Containers run as a non-root user.
- Docker Compose uses healthchecks and `restart: unless-stopped`.

## HTTP

- `/livez` is process liveness.
- `/readyz` is readiness for traffic.
- Every response includes `X-Request-Id`.
- Logs include method, path, status, request ID, and duration.
- Logs support `stdout`, `file`, or `both` via `LOG_OUTPUT`.
- File logs use JSON lines and should default under `logs/<service>.log`.
- Panic recovery returns a generic `500`.
- Security headers are set by default.

## Verification

- `make fmt`
- `make tidy`
- `make test`
- `make build`
