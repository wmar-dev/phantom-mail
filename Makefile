# Phantom Mail developer commands. CI runs exactly these targets.
BIN := bin/phantom-mail
IMAGE ?= phantom-mail

.PHONY: build run test test-go test-web lint bench docs-check docker up test-docker clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/phantom-mail

run:
	go run ./cmd/phantom-mail

test: lint test-go test-web

test-go:
	go test -race ./...

# Web UI logic tests use Node's built-in runner (no npm packages).
test-web:
	@if command -v node >/dev/null 2>&1; then node --test internal/web/static/*.test.mjs; else echo "node not found: skipping web tests (use 'docker compose run --rm test')"; fi

lint:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...

# Strict performance gates (the plan's numbers) first, alone on the machine,
# then the benchmarks.
bench:
	PM_PERF_STRICT=1 go test -count=1 -v -run 'Gate|Fifty|Startup' ./tests/bench/... | grep -E 'ingest:|p95|rebuilt|heap|^(ok|FAIL|---)'
	go test -run '^$$' -bench . -benchmem ./tests/bench/...

# Passes vacuously until tests/docs contains tests.
docs-check:
	@if ls tests/docs/*_test.go >/dev/null 2>&1; then go test ./tests/docs/...; else echo "no docs tests yet"; fi

docker:
	docker build --target runtime -t $(IMAGE) .

# Run the service in Docker (web :8080, SMTP :2525).
up:
	docker compose up --build

# Run the whole suite inside Docker; only Docker is required.
test-docker:
	docker compose run --rm --build test

clean:
	rm -rf bin
