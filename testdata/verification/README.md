# M1-E2 verifier test data

`m1-fixture-20261009T162703Z-4ddb7a/` is the M1-E1 accepted fixture bundle
(`artifacts/m1/m1-fixture-20261009T162703Z-4ddb7a/`, produced at `0853b44`)
with its 24 browser screenshots left out to keep the repository small:

- every other file is copied byte for byte (its digest in the manifest is
  the producer's);
- `manifest.json` lists the kept files only (the PNG entries removed,
  nothing else changed); `run-end.json` carries that manifest's digest.

So this directory is not the producer's manifest; it is a fixed input for
`internal/verification` tests.

`expected.json` is what the checker must report for it: written from the
frozen `testdata/contracts/cases.json` (expected outcomes) and the M1-E1
accepted layers (13 host E2E, 3 Store, 15 contracts;
`docs/m1-evidence.md` section 4), not by running the checker or the
producer's checks.

The tests change copies of this directory (in temporary directories) to
make the negative cases; the directory itself is only read.
