# M1-E3 live checker test data (synthetic)

`m1-live-20261010T041827Z-50f565/` is a **synthetic test vector, not a real
run**. `cmd/m1-live` made it with a stand-in for the Claude Code CLI: a
script that answered `--version` with the approved version and otherwise ran
the fake CLI (`cmd/fake-claude`: no model, no network) on a scenario with a
live-shaped init (dev demo plugins, auth `none`) and the main line
(list_products, get_quote, request_purchase). HOME was a new directory with
an empty `.apa-claude`; the repository a local clone at `f0adfb3`. The paths
in its command and build record are that scratch setup's.

It is copied byte for byte (no file changed, the manifest and end marker are
the producer's). `expected.json` is what the checker must report for it in
live mode, written by hand from the frozen case list (ask: Ask,
HUMAN_APPROVAL_REQUIRED, pending; 30 cases and 7 capabilities not
evaluated), not by running the checker.
