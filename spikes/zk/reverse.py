#!/usr/bin/env python3
"""Z1 spike reverse checks: remove one constraint in a disposable copy of
this module and require the named test to fail with its expected
assertion (a build error, a refused compile or any other failure does not
count).

  python3 -I spikes/zk/reverse.py OUT_DIR [name regex]
"""
import os, re, shutil, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = "zk.go"
ITEMS = [  # (name, test filter, expected regex, old, new)
    ("amount in range (no negative amount)", "^TestProverRefuses$/^a_negative_amount", r"a negative amount \(the balance grows\): a proof was made",
     "\tapi.ToBinary(c.Amount, 64)\n", ""),
    ("balance_new in range (no over-budget spend)", "^TestProverRefuses$/^over_budget", r"over budget \(balance_new wraps\): a proof was made",
     "\tapi.ToBinary(balanceNew, 64) // non-negative: a wrapped difference is about 2^254\n", ""),
    ("the old commitment opens", "^TestProverRefuses$/^a_balance_above", r"a balance above the committed one: a proof was made",
     "api.AssertIsEqual(c.COld, old)", "api.AssertIsEqual(old, old)"),
    ("the new commitment holds the rest", "^TestProverRefuses$/^C_new_not_the_rest", r"C_new not the rest: a proof was made",
     "api.AssertIsEqual(c.CNew, next)", "api.AssertIsEqual(next, next)"),
    ("the nullifier is the task's", "^TestProverRefuses$/^another_task's_nullifier", r"another task's nullifier: a proof was made",
     "api.AssertIsEqual(c.Nullifier, n)", "api.AssertIsEqual(n, n)"),
    ("the intent's hi limb is constrained", "^TestTamperedPublicInputs$/^another_intent_\\(hi_limb\\)", r"another intent \(hi limb\): verified",
     "\tapi.ToBinary(c.IntentHi, 128)\n", ""),
]


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    env = dict(os.environ, GOFLAGS="-mod=readonly", GOPROXY="off", GOCACHE=os.environ.get("GOCACHE") or os.path.join(tempfile.gettempdir(), "gocache"))
    caught = 0
    only = re.compile(sys.argv[2]) if len(sys.argv) > 2 else None
    items = [(n, it) for n, it in enumerate(ITEMS, 1) if not only or only.search(it[0])]
    for n, (name, run, rx, old, new) in items:
        tmp = tempfile.mkdtemp(prefix="z1rev-")
        copy = os.path.join(tmp, "zk")
        shutil.copytree(HERE, copy, ignore=shutil.ignore_patterns("reverse.py", "__pycache__"))
        path = os.path.join(copy, SRC)
        text = open(path).read()
        if text.count(old) != 1:
            result, why = "not-caught", f"anchor found {text.count(old)} times"
            log = why
        else:
            open(path, "w").write(text.replace(old, new))
            p = subprocess.run(["go", "test", "-count=1", "-run", run, "."], cwd=copy, env=env, capture_output=True, text=True, timeout=600)
            log = p.stdout + p.stderr
            if "[build failed]" in log or "[setup failed]" in log:
                result, why = "not-caught", "build failed"
            elif p.returncode != 1 or not re.search(rx, log):
                result, why = "not-caught", f"exit {p.returncode}; expected /{rx}/ not found"
            else:
                result, why = "caught", re.search(rx, log).group(0)
                caught += 1
        open(os.path.join(out, f"{n:02d}-{name.replace(' ', '-')}.log"), "w").write(f"item: {name}\nrun: {run}\nexpects: {rx}\nmutation: {old!r} -> {new!r}\n\n{log}")
        print(f"RESULT {result} | {name} | {why}")
    print(f"{caught}/{len(items)} caught by the expected assertion")
    return 0 if caught == len(items) else 1


if __name__ == "__main__":
    sys.exit(main())
