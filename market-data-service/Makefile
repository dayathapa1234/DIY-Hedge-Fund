.PHONY: build test test-integration tidy fmt run docker-up docker-down docker-build

build:
	go build ./cmd/market-data-service

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

tidy:
	go mod tidy

fmt:
	gofmt -w .

run:
	go run ./cmd/market-data-service

docker-up:
	docker compose -f ../docker-compose.yml up --build

docker-down:
	docker compose -f ../docker-compose.yml down

docker-build:
	docker compose -f ../docker-compose.yml build
