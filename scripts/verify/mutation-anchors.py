"""Check that every mutation of scripts/verify/mutation-check.py still names
text found exactly once in its file, by the checker's own rule (LINE:
markers count lines), without running any test. A quick local guard before a
full run: a changed line makes a mutation ERROR, not KILLED.

    python3 scripts/verify/mutation-anchors.py
"""
import importlib.util
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("mc", root / "scripts/verify/mutation-check.py")
mc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mc)
bad = []
for n, (finding, rel, old, _new, _test) in enumerate(mc.MUTATIONS, 1):
    text = (root / rel).read_text()
    count = sum(old[5:] in l for l in text.splitlines()) if old.startswith("LINE:") else text.count(old)
    if count != 1:
        bad.append(f"{n:02d} {finding}: found {count} times in {rel}")
print(f"{len(mc.MUTATIONS)} mutations: " + ("every anchor found once" if not bad else f"{len(bad)} not found once"))
print("\n".join(bad))
sys.exit(1 if bad else 0)
