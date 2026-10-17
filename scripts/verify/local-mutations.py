#!/usr/bin/env python3
"""Runs mutation-check.py's LOCAL_MUTATIONS (or, with --all, every entry
whose finding starts with PREFIX) here, not in a container: each in a fresh
copy of the working tree's tracked and new files, with var/ linked in for
the local chain tools (anvil, the contract build). Never in the tree itself.
Each test is first run once unmutated in the copy and must pass there
(not skip): otherwise a broken environment would read as KILLED. A test
that skips is an error, never a pass.

    python3 -I scripts/verify/local-mutations.py [PREFIX] [--all] [-v: the log of each one not killed]

An entry's test may name another package, "internal/budget:TestName", when
the mutated code (e.g. internal/eth's) is exercised by another's tests.
"""
import importlib.util, os, pathlib, shutil, subprocess, sys, tempfile

root = pathlib.Path(__file__).resolve().parent.parent.parent
args = [a for a in sys.argv[1:] if a not in ("--all", "-v")]
prefix = args[0] if args else ""
spec = importlib.util.spec_from_file_location("mc", root / "scripts/verify/mutation-check.py")
mc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mc)
entries = mc.MUTATIONS + mc.LOCAL_MUTATIONS if "--all" in sys.argv else mc.LOCAL_MUTATIONS
files = subprocess.run(["git", "-C", str(root), "ls-files", "-co", "--exclude-standard"], capture_output=True, text=True, check=True).stdout.split()
work = pathlib.Path(tempfile.mkdtemp())
tree = work / "t"


def fresh():
    shutil.rmtree(tree, ignore_errors=True)
    for f in files:
        if f.startswith(("var/", "demo/", "artifacts/")) or not (root / f).is_file():
            continue
        d = tree / f
        d.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(root / f, d)
    os.symlink(root / "var", tree / "var")


def target(m):
    """(package, test) an entry runs: its own file's package unless named."""
    if ":" in m[4]:
        return tuple(m[4].split(":", 1))
    return os.path.dirname(m[1]), m[4]


bad = 0
chosen = [m for m in entries if m[0].startswith(prefix)]
for pkg, test in sorted({target(m) for m in chosen}):
    fresh()
    r = subprocess.run(["go", "test", "./" + pkg, "-run", test, "-count=1", "-v", "-timeout", "300s"], cwd=tree, capture_output=True, text=True)
    ok = r.returncode == 0 and "--- SKIP" not in r.stdout + r.stderr
    print(f"{'BASELINE' if ok else 'NO BASE '} ./{pkg} -run {test}", flush=True)
    if not ok:
        print((r.stdout + r.stderr)[-1500:])
        sys.exit(1)
for m in chosen:
    finding, rel, old, new, _ = m
    pkg, test = target(m)
    fresh()
    p = tree / rel
    t = p.read_text()
    if t.count(old) != 1:
        print(f"ERROR    {finding}: found {t.count(old)} times")
        bad += 1
        continue
    p.write_text(t.replace(old, new))
    r = subprocess.run(["go", "test", "./" + pkg, "-run", test, "-count=1", "-v", "-timeout", "300s"],
                       cwd=tree, capture_output=True, text=True)
    log = r.stdout + r.stderr
    if "--- SKIP" in log:
        verdict = "SKIPPED"
    elif r.returncode and "--- FAIL" in log and "panic: test timed out" not in log:
        verdict = "KILLED"
    else:
        verdict = "BUILD?" if r.returncode else "SURVIVED"
    bad += verdict != "KILLED"
    print(f"{verdict:8s} {finding}", flush=True)
    if verdict != "KILLED" and "-v" in sys.argv:
        print("\n".join(l for l in log.splitlines() if "_test.go" in l or "internal/budget/" in l or l.startswith("goroutine"))[-3000:])
shutil.rmtree(work, ignore_errors=True)
sys.exit(1 if bad else 0)
