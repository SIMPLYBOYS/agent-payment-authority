#!/usr/bin/env python3
"""Runs mutation-check.py's LOCAL_MUTATIONS (or, with --all, every entry
whose finding starts with PREFIX) here, not in a container: each in a fresh
copy of the working tree's tracked and new files, with var/ linked in for
the local chain tools (anvil, the contract build). Never in the tree itself.
A test that skips is an error, never a pass.

    python3 -I scripts/verify/local-mutations.py [PREFIX] [--all] [-v: the log of each one not killed]
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


bad = 0
for finding, rel, old, new, test in [m for m in entries if m[0].startswith(prefix)]:
    fresh()
    p = tree / rel
    t = p.read_text()
    if t.count(old) != 1:
        print(f"ERROR    {finding}: found {t.count(old)} times")
        bad += 1
        continue
    p.write_text(t.replace(old, new))
    r = subprocess.run(["go", "test", "./" + os.path.dirname(rel), "-run", test, "-count=1", "-v", "-timeout", "300s"],
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
