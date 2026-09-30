.PHONY: check check-contracts build dev

# Compile, vet and unit-test. Calls no paid model and touches no testnet.
check:
	go vet ./...
	go test ./... -count=1
	go build ./...

# Go contracts and independent display-adapter vectors share fixed fixtures.
check-contracts:
	go test ./internal/contracts/... -count=1
	node scripts/check-contract-vectors.mjs

build:
	go build -o bin/demo ./cmd/demo

# Builds first, loads .env if present, then replaces the shell with the server
# so Ctrl-C reaches it directly. Starts nothing outside this repo.
#
# .env is checked before it is sourced: every line must be blank, a # comment,
# or KEY=VALUE with no spaces, quotes or shell syntax. One bad line stops the
# start, so the server never runs on a half-loaded file. Only line numbers are
# printed, never values.
dev: build
	@set -e; \
	if [ -f .env ]; then \
		bad=$$(grep -nvE '^([[:space:]]*(#.*)?|[A-Za-z_][A-Za-z0-9_]*=[][A-Za-z0-9_.:/@%+,-]*)$$' .env | cut -d: -f1 | tr '\n' ' '); \
		if [ -n "$$bad" ]; then \
			echo "dev: .env line(s) $${bad}are not plain KEY=VALUE (no spaces, quotes or shell syntax); nothing was started" >&2; \
			exit 1; \
		fi; \
		set -a; . ./.env; set +a; \
	fi; \
	exec ./bin/demo
