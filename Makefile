# The gate. `make check` is the single command that decides whether a change is
# good. It is what CI runs and what the agent loop will grade itself against.
# Keep it fast and hermetic. A repo whose gate cannot be made to fail on a real
# regression has no gate at all.

.PHONY: check fmt vet build test tidy

check: fmt vet build test ## The full gate (== CI): gofmt + go vet + build + test

fmt: ## Fail if any file needs gofmt.
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; \
	fi

vet: ## go vet across all packages.
	go vet ./...

build: ## Build all packages.
	go build ./...

test: ## Run all tests.
	go test ./...

tidy: ## Tidy the module graph.
	go mod tidy
