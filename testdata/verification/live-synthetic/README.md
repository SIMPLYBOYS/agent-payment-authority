# M1-E3 live checker test data (synthetic)

`m1-live-20261010T151404Z-9f8031/` is a **synthetic test vector, not a real
run**. `cmd/m1-live` made it with a stand-in for the Claude Code CLI: a
script that answered `--version` with the approved version and otherwise ran
the fake CLI (`cmd/fake-claude`: no model, no network) on a scenario with a
live-shaped init (dev demo plugins, auth `none`) and the main line
(list_products, get_quote, request_purchase). HOME was a new directory with
an empty `.apa-claude`; the repository a local clone at `0cb0fa4` (run record v2). The paths
in its command and build record are that scratch setup's.

It is copied byte for byte (no file changed, the manifest and end marker are
the producer's). `expected.json` is what the checker must report for it in
live mode, written by hand from the frozen case list (ask: Ask,
HUMAN_APPROVAL_REQUIRED, pending; 30 cases and 7 capabilities not
evaluated), not by running the checker.

It replaces the first vector, `m1-live-20261010T041827Z-50f565/` (run
record v1, removed in the E3 fix round). That one contradicted itself: the
wrapper ended by itself with `apa_buyer_exit_code` 0, yet `producer_error`
said "the outer bound was reached; the run was stopped", because the old
producer released the run's context before it asked the context why the
run ended (docs/m1-evidence.md 16.6, R2). The v1 checker did not read
`producer_error` and passed it; the v2 checker fails such a record.
