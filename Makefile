# The gate. `make check` is the single command that decides whether a change is
# good. It is what CI runs and what the agent loop will grade itself against.
# Keep it fast and hermetic. A repo whose gate cannot be made to fail on a real
# regression has no gate at all.

.PHONY: check fmt vet workflows build test tidy

check: fmt vet workflows build test ## The full gate (== CI): gofmt + go vet + workflows + build + test

fmt: ## Fail if any file needs gofmt.
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; \
	fi

workflows: ## Workflow YAML parses, no conflict markers, no disabled safety controls.
	@./scripts/check-workflows.sh

vet: ## go vet across all packages.
	go vet ./...

# -buildvcs=false: the gate builds throwaway binaries, so stamping git metadata
# into them buys nothing and couples the gate to git plumbing it does not need.
# It also breaks the agent loop outright. The loop works in a worktree under the
# temp directory, and Go's VCS stamping fails there with
# "error obtaining VCS status: exit status 128", before any repo code compiles.
# A gate that cannot run where the loop runs is not a gate. Release binaries are
# built separately in .github/workflows/release.yml and take their version from
# -ldflags, not from stamping, so nothing user-visible depends on this.
build: ## Build all packages.
	go build -buildvcs=false ./...

test: ## Run all tests and write a coverage profile for CI upload.
	go test -buildvcs=false -race -coverprofile=coverage.txt -covermode=atomic ./...

tidy: ## Tidy the module graph.
	go mod tidy
