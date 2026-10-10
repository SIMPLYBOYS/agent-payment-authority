# M1 tests: entry points and case map

M1 has no separate test framework: the cases and checks below are the
existing package tests and commands. This page only says where each is.

| What | Entry | Tests |
|---|---|---|
| Fixture bundle (all 31 cases) | `make m1-fixture` | `cmd/m1-fixture`, `cmd/demo` `TestM1FixtureCases` (13 host E2E), `internal/store` `TestAcceptedIntegrationCases` (16), `internal/contracts` `TestContractCasesFromManifest` (15), `internal/policy` `TestAcceptedPolicyCases`, `internal/m1evidence` |
| Live single-Ask bundle | `make m1-live-cli` (one real run, separately approved) | `cmd/m1-live` (a stand-in CLI over the fake; preconditions, failed runs, save failure) |
| Read-only checker | `make verify-m1 RUN_DIR=... [MODE=live]` | `internal/verification` (fixture: golden, negatives, paths, requirements; live: `live_test.go`), `cmd/verify-demo` (exit codes) |
| Reverse checks | `python3 scripts/verify/m1-e1-reverse.py`, `m1-e2-reverse.py`, `m1-e3-reverse.py` | remove one key check at a time; an assertion must fail |

Which case runs at which layer, and what each bundle claims:
`docs/m1-evidence.md` sections 4 (fixture), 15 (checker) and 16 (live).
