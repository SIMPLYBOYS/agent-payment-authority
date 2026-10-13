.PHONY: check check-m1-e3 check-contracts build dev demo-fake demo-integration demo present-live present-fake present-rehearsal present-p1a present-a2 m1-fixture verify-m1 m1-live-cli

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
	node --test web/test/hostcreds.test.mjs  # /host credentials (docs/demo-operation-design.md 6.3)
	node --test web/test/scene.test.mjs  # /host scene with a slot per customer (docs/multi-agent-demo-design.md 5)
	node --test web/test/p1a.test.mjs  # P1-a: apa.pay.v1 in the page and the simulated approver (docs/commercial-p1a.md)
	node --test web/test/approve.test.mjs  # A2: the person's approval in the page (docs/commercial-a2.md)

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

# B1-06 slices 3.1/3.2: the fake demo in one command, with checks of the
# Unity build and the LimeZu import; the host's page is
# http://127.0.0.1:8402/host, where runs are started (scenario catalog, demo
# registry, its own new database). See docs/demo-operation-design.md.
demo:
	sh scripts/demo/start.sh

# docs/cli-poc.md 56: one way to start a demo and open /host. A new session
# directory each time; the host shows a one-time pairing code on this
# terminal; a run starts only when the host presses 「開始」 on the page.
# present-live: the real Claude Code CLI (one-time setup: var/present.conf);
# present-fake: the fake CLI, no model.
present-live:
	@sh scripts/demo/present.sh live

present-fake:
	@sh scripts/demo/present.sh fake

# present-rehearsal: the fake demo with the rehearsal catalog (runs that wait
# until stopped, and the main line), for the UX1 manual checks
# (docs/commercial-ux1.md 7.2). Fake CLI only.
present-rehearsal:
	@APA_PRESENT_SCENARIOS=testdata/demo/scenarios-rehearsal.json sh scripts/demo/present.sh fake

# present-p1a: the fake demo with the P1-a catalog: confirm a delegation as
# the simulated customer, the Agent asks, approve or deny (simulated; no
# payment). docs/commercial-p1a.md has the steps. Fake CLI only.
present-p1a:
	@APA_PRESENT_SCENARIOS=testdata/demo/scenarios-p1a.json sh scripts/demo/present.sh fake

# present-a2: the P1-a catalog with the person's approval by a registered
# device (docs/commercial-a2.md): SIGNER is the socket of a running
# apa-signer serve-approvals whose origin is http://localhost:8402; the page
# opened is http://localhost:8402/host. Fake CLI only; payment stays off.
# AUTH=virtual declares a virtual authenticator (default device).
present-a2:
	@test -n "$(SIGNER)" || { echo "usage: make present-a2 SIGNER=/path/of/serve-approvals.sock (docs/commercial-a2.md)" >&2; exit 2; }
	@APA_ADDR=127.0.0.1:8402 APA_APPROVALS_SIGNER="$(SIGNER)" APA_AUTHENTICATOR_SOURCE="$(AUTH)" APA_PRESENT_SCENARIOS=testdata/demo/scenarios-p1a.json sh scripts/demo/present.sh fake

# docs/m1-evidence.md: the M1 fixture evidence bundle, a new
# artifacts/m1/<run-id>/ each time (manifest.json, its evidence, run-end.json).
# Fake CLI only: no model, no testnet. M1_FIXTURE_FLAGS=-skip-browser leaves
# out the browser checks (they then are missing from the bundle).
m1-fixture:
	go run ./cmd/m1-fixture $(M1_FIXTURE_FLAGS)

# M1-E2 (docs/m1-evidence.md section 15): the read-only evidence checker.
# Reads RUN_DIR and the frozen case list; never runs m1-fixture, a model or
# the bundle's commands. The JSON report goes to stdout; the checker exits
# 0 PASS, 1 FAIL, 2 INCONCLUSIVE, 3 not checked, which make reports as its
# own "Error N" (exit 2): read the report's verdict.
# MODE=live checks a live single-Ask bundle (make m1-live-cli).
MODE ?= fixture
verify-m1:
	@test -n "$(RUN_DIR)" || { echo "usage: make verify-m1 RUN_DIR=artifacts/m1/<run-id> [MODE=fixture|live]" >&2; exit 2; }
	@go run -buildvcs=true ./cmd/verify-demo --run-dir "$(RUN_DIR)" --mode "$(MODE)"

# M1-E3 (docs/m1-evidence.md section 16): ONE real Claude Code run (a real
# model on the APA-only login) through apa-buyer's live path, into a new
# artifacts/m1/<run-id>/. Only with Aaron's separate approval of that one
# run. Needs APA_LIVE_CLI=true in the environment and all three of CLI (the
# absolute path), BASE (the reviewed product commit; HEAD may differ in
# docs/ and README.md only) and GATE (dev_demo_plugins or strict). A clean
# local clone is built; nothing retries. The producer exits 0 ask PASS, 1
# the run or its record failed, 2 stopped before the CLI or not saved
# completely (make reports a non-zero exit as its own Error N).
m1-live-cli:
	@test -n "$(CLI)" && test -n "$(BASE)" && test -n "$(GATE)" || { echo "usage: APA_LIVE_CLI=true make m1-live-cli CLI=/abs/path/claude BASE=<reviewed commit> GATE=dev_demo_plugins|strict" >&2; exit 2; }
	@go run -buildvcs=true ./cmd/m1-live --live --cli "$(CLI)" --base "$(BASE)" --gate "$(GATE)"

# M1-E3 fix rounds (docs/m1-evidence.md 16.6, 16.7): the targeted fake checks
# and the R1-R4 reverse items (each on its own test, in a disposable clone),
# with progress on stderr and the JSON summary on stdout, into a new
# artifacts/m1-check/m1-check-<time>/. No real CLI. ARGS=--list prints the
# plan, the reverse items' filters and expectations, and runs nothing. Exit
# 2 interrupted, else 1 a step did not pass, else 3 the evidence or the
# summary incomplete, else 0. Built first so a signal reaches the runner
# itself (not go run).
check-m1-e3:
	@go build -o "$${TMPDIR:-/tmp}/apa-m1-check" ./cmd/m1-check && exec "$${TMPDIR:-/tmp}/apa-m1-check" $(ARGS)
