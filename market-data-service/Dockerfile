FROM golang:1.24-bookworm AS build

ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_TIME=unknown

WORKDIR /src
COPY go.mod go.sum ./
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT_SHA} -X main.buildTime=${BUILD_TIME}" -o /out/market-data-service ./cmd/market-data-service

FROM python:3.12-slim

WORKDIR /app
RUN pip install --no-cache-dir yfinance==0.2.65
COPY --from=build /out/market-data-service /usr/local/bin/market-data-service
COPY scripts ./scripts

RUN useradd --create-home --shell /usr/sbin/nologin appuser

ENV ADDR=:8080
ENV PROVIDER_PRIORITY=stooq,yfinance,openbb
ENV YFINANCE_PYTHON=python3
ENV YFINANCE_SCRIPT=/app/scripts/yfinance_fetch.py

EXPOSE 8080
USER appuser
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/readyz', timeout=3).read()"
ENTRYPOINT ["market-data-service"]
