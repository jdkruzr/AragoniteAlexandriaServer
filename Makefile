.PHONY: build test check compose-up compose-down

build:
	go build ./cmd/loom

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...

compose-up:
	docker compose -f deploy/compose/compose.yml up --build -d

compose-down:
	docker compose -f deploy/compose/compose.yml down
