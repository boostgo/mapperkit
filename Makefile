GO ?= go

.PHONY: all test race lint cover bench examples tidy

all: lint test

test: ## Run tests with the race detector
	$(GO) vet ./...
	$(GO) test -race ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

cover: ## Produce a coverage report (coverage.html)
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html

bench: ## Run benchmarks
	$(GO) test -run=^$$ -bench=. -benchmem ./...

examples: ## Run all example programs
	@for d in examples/*/; do echo "== $$d"; $(GO) run ./$$d || exit 1; done

tidy:
	$(GO) mod tidy
