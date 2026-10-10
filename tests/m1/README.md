# M1 tests: entry points and case map

M1 has no separate test framework: the cases and checks below are the
existing package tests and commands. This page only says where each is.

| What | Entry | Tests |
|---|---|---|
| Fixture bundle (all 31 cases) | `make m1-fixture` | `cmd/m1-fixture`, `cmd/demo` `TestM1FixtureCases` (13 host E2E), `internal/store` `TestAcceptedIntegrationCases` (16), `internal/contracts` `TestContractCasesFromManifest` (15), `internal/policy` `TestAcceptedPolicyCases`, `internal/m1evidence` |
| Live single-Ask bundle | `make m1-live-cli` (one real run, separately approved) | `cmd/m1-live` (a stand-in CLI over the fake; preconditions, failed runs, save failure, start failure, SIGINT/SIGTERM, heartbeats) |
| Read-only checker | `make verify-m1 RUN_DIR=... [MODE=live]` | `internal/verification` (fixture: golden, negatives, paths, requirements; live: `live_test.go`), `cmd/verify-demo` (exit codes) |
| E3 fix rounds, targeted (fake only, with progress) | `make check-m1-e3` (`ARGS=--list` prints the plan, the reverse items' filters and expectations) | `cmd/m1-check` (runner), `internal/progress`, `scripts/verify/m1_e3_reverse_test.py` (classifier), plus the producer, checker and R1-R4 reverse steps it runs |
| Reverse checks | `python3 scripts/verify/m1-e1-reverse.py`, `m1-e2-reverse.py`, `m1-e3-reverse.py --out DIR [regex]` | remove one key check at a time; an assertion must fail (E3 since 10/11: the item's own test with its expected assertion, in a disposable clone; items from before 10/11 have no expectation yet and are not counted as caught) |

Which case runs at which layer, and what each bundle claims:
`docs/m1-evidence.md` sections 4 (fixture), 15 (checker) and 16 (live).

Accepted bundles and how each card item maps to evidence: `docs/m1-result.md`
(sections 3 and 5). The fixture bundle (31 cases) and the live bundle (the
single `ask`) are judged separately and do not stand in for each other; the
E4 clean-directory fixture bundle is in `artifacts/m1-e4-clean/` (section 3.4).
