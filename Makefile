.PHONY: check check-contracts build dev demo-fake demo-integration

# Compile, vet and unit-test. Calls no paid model and touches no testnet.
check:
	go vet ./...
	go test ./... -count=1
	go build ./...

# Go contracts and independent display-adapter vectors share fixed fixtures;
# the Dashboard's event adapter is tested against the same accepted cases.
check-contracts:
	go test ./internal/contracts/... -count=1
	node scripts/check-contract-vectors.mjs
	node --test web/test/adapter.test.mjs  # Dashboard adapter (docs/demo-ui.md); the same Node

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

# FAKE MODE ONLY: the fake CLI runs no model and makes no network request.
# Real subprocesses, a real loopback HTTP 402 and a real SQLite Store at
# APA_DB_PATH (default var/apa-fake.db). A task runs once per database; pick
# another with TASK=task-a-next or set APA_DB_PATH to a new file.
demo-fake:
	go build -o bin/apa-buyer ./cmd/apa-buyer
	go build -o bin/fake-claude ./cmd/fake-claude
	./bin/apa-buyer run --fake-cli bin/fake-claude --scenario testdata/cli/ask-mainline.json --task $(or $(TASK),task-a)

# B1-06 delivery 1, FAKE MODE ONLY: one demo host process runs the shop
# (HTTP 402), the Store (APA_DB_PATH, default var/apa-demo-fake.db), its
# expiry worker, the authenticated read-only task API and the fake buyer
# for each task of the trusted plan. Demo tokens: var/demo-tokens.json
# (0600, new at each start). See docs/demo-integration.md.
demo-integration:
	go build -o bin/demo ./cmd/demo
	go build -o bin/fake-claude ./cmd/fake-claude
	APA_ADDR=$(or $(APA_ADDR),127.0.0.1:8402) ./bin/demo --fake-cli bin/fake-claude --plan testdata/demo/fake-plan.json
