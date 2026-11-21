
BINARY := darkubectl
BIN_DIR := bin

.DEFAULT_GOAL := help

.PHONY: help build binary install test vet lint lint-md fmt fmt-check check clean

## @group General

## Show available commands
help:
	@color=0; \
		if [ -t 1 ] && [ "$${TERM:-dumb}" != dumb ] && [ -z "$${NO_COLOR:-}" ]; then color=1; fi; \
		awk -v color="$$color" '\
		BEGIN { \
			group = "Other"; desc = ""; width = 14; \
			if (color) { bold = "\033[1m"; cyan = "\033[36m"; reset = "\033[0m"; } \
		} \
		/^## @group / { \
			group = substr($$0, 11); \
			desc = ""; \
			next; \
		} \
		/^## / { \
			desc = substr($$0, 4); \
			next; \
		} \
		/^[a-zA-Z0-9_.-]+[[:space:]]*:/ { \
			if (desc != "") { \
				target = $$1; \
				sub(/:.*/, "", target); \
				n++; \
				groups[n] = group; \
				targets[n] = target; \
				descs[n] = desc; \
				if (length(target) > width) \
					width = length(target); \
			} \
			desc = ""; \
			next; \
		} \
		/^[^[:space:]#]/ { desc = ""; } \
		END { \
			printf "\n%sDarkubectl development%s\n", bold, reset; \
			for (i = 1; i <= n; i++) { \
				if (groups[i] != previous) { \
					printf "\n%s%s%s\n", bold, groups[i], reset; \
					previous = groups[i]; \
				} \
				printf "  %s%-*s%s %s\n", cyan, width, targets[i], reset, descs[i]; \
			} \
			printf "\n"; \
		}' $(firstword $(MAKEFILE_LIST))

## @group Build

## Build all Go packages
build:
	go build ./...

## Build the CLI binary
binary:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) .

## Install the CLI
install:
	go install .

## @group Development

## Format Go source files
fmt:
	go fmt ./...

## @group Quality

## Run tests with the race detector
test:
	go test -race ./...

## Run Go static analysis
vet:
	go vet ./...

## Run golangci-lint
lint:
	golangci-lint run ./...

## Lint Markdown files (requires npx)
lint-md:
	npx markdownlint-cli2 "**/*.md"

## Check Go source formatting
fmt-check:
	@unformatted="$$(find . -name '*.go' \
		-not -path './vendor/*' \
		-exec gofmt -l {} +)"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Go files need formatting:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

## Run all quality checks
check: fmt-check vet lint test build

## @group Maintenance

## Remove compiled binaries
clean:
	rm -rf $(BIN_DIR)

