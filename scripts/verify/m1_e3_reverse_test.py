#!/usr/bin/env python3
# Fast fake tests of m1-e3-reverse.py's classifier (docs/m1-evidence.md
# 16.7): synthetic go test -json output, no build of the product. Only the
# expected test failing with its expected assertion is caught; a timeout
# panic, another panic, a build or environment failure, a test that did
# not run, a different assertion, a signal and a success are not.
#   python3 -I scripts/verify/m1_e3_reverse_test.py
import importlib.util, json, os

spec = importlib.util.spec_from_file_location("reverse", os.path.join(os.path.dirname(os.path.abspath(__file__)), "m1-e3-reverse.py"))
rev = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rev)

ITEM = rev.go("./x/", "^TestA$", ("TestA/sub", r"exit 0, "))


def stream(*evs, tail=""):
    return "".join(json.dumps(dict(zip(("Action", "Test", "Output"), e))) + "\n" for e in evs) + tail


def failing(output):
    return stream(("run", "TestA"), ("run", "TestA/sub"), ("output", "TestA/sub", output), ("fail", "TestA/sub"), ("fail", "TestA"))


CASES = [
    # (name, rc, text, timed out, caught)
    ("the expected assertion", 1, failing("    main_test.go:12: exit 0, 1 summaries\n"), False, True),
    # go test exits 1 when its test binary panics, and the expected line
    # may already be there (t.Errorf before the panic): only the panic
    # check stands between these and "caught".
    ("a timeout panic", 1, stream(("run", "TestA"), ("run", "TestA/sub"), ("output", "TestA/sub", "    main_test.go:12: exit 0, 1 summaries\n"),
                                  ("output", "TestA/sub", "panic: test timed out after 1s\n"), ("fail", "TestA/sub")), False, False),
    ("a panic", 1, stream(("run", "TestA"), ("run", "TestA/sub"), ("output", "TestA/sub", "    main_test.go:12: exit 0, 1 summaries\n"),
                          ("output", "TestA/sub", "panic: close of nil channel [recovered]\n"), ("fail", "TestA/sub")), False, False),
    ("a failure without the expected assertion", 1, failing("    main_test.go:12: something else\n"), False, False),
    ("a build failure", 1, stream(("build-fail", "", "")) + "FAIL\tx [build failed]\n", False, False),
    ("a setup failure", 1, "FAIL\tx [setup failed]\n", False, False),
    ("the expected test did not run", 1, stream(("run", "TestB"), ("output", "TestB", "    b_test.go:3: exit 0, \n"), ("fail", "TestB")), False, False),
    ("the expected test passed", 0, stream(("run", "TestA"), ("run", "TestA/sub"), ("pass", "TestA/sub"), ("pass", "TestA")), False, False),
    ("a timeout of the script", -9, failing("    main_test.go:12: exit 0, \n"), True, False),
    ("a signal", -15, failing("    main_test.go:12: exit 0, \n"), False, False),
    ("another exit code", 2, failing("    main_test.go:12: exit 0, \n"), False, False),
]

# A timeout panic first: the R4 reverse item that drops the panic check
# must fail on it.
for name, rc, text, timed_out, want in CASES:
    caught, reason = rev.classify(ITEM, rc, text, timed_out)
    assert caught == want, f"{name} counted as {'caught' if caught else 'not caught'} ({reason})"

PY = rev.py("t.py", r"the thing broke")
for name, rc, text, want in [
    ("the expected AssertionError", 1, "Traceback (most recent call last):\n  ...\nAssertionError: the thing broke\n", True),
    ("another AssertionError", 1, "Traceback (most recent call last):\nAssertionError: other\n", False),
    ("another exception", 1, "Traceback (most recent call last):\nKeyError: 'x'\n", False),
    ("passed", 0, "", False),
]:
    caught, reason = rev.classify(PY, rc, text)
    assert caught == want, f"python {name} counted as {'caught' if caught else 'not caught'} ({reason})"

# The baseline: an unmutated run passes only when every expected test passed.
assert rev.passed(ITEM, 0, stream(("run", "TestA/sub"), ("pass", "TestA/sub"))), "a passing baseline rejected"
assert not rev.passed(ITEM, 0, stream(("run", "TestB"), ("pass", "TestB"))), "a baseline without the expected test accepted"

# Every 10/11 item names its expected test and assertion; every anchor is in
# the source exactly once.
root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
for name, t, edits in rev.MUTATIONS:
    if name.startswith(("R1 ", "R2 ", "R3 ", "R4 ")):
        assert t["expect"], f"{name} has no expected assertion"
    for path, old, _ in edits:
        n = open(os.path.join(root, path)).read().count(old)
        assert n == 1, f"{name}: anchor in {path} found {n} times"
print(f"{len(CASES) + 6} classifier cases and {len(rev.MUTATIONS)} items' anchors pass")
