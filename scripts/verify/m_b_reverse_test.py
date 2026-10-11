#!/usr/bin/env python3
# Fast checks of m-b-reverse.py's Node and Unity classifiers (the Go one is
# m1-e3-reverse.py's, checked by m1_e3_reverse_test.py), and that every item's
# anchor is in the source exactly once. No build, no test run.
#   python3 -I scripts/verify/m_b_reverse_test.py
import importlib.util, os

here = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("mb", os.path.join(here, "m-b-reverse.py"))
mb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mb)

NAME = "the six-field rule: only the exact projection last sent completes"
node = mb.node("web/test/scene.test.mjs", NAME, r"another slot")
tap = "not ok 3 - " + NAME + "\n  ---\n  error: 'another slot'\n  code: 'ERR_ASSERTION'\n  ...\nok 4 - other\n# tests 4\n"
for name, rc, text, want in [
    ("the expected assertion", 1, tap, True),
    ("another error", 1, tap.replace("ERR_ASSERTION", "ERR_MODULE_NOT_FOUND"), False),
    ("another message", 1, tap.replace("another slot", "another task"), False),
    ("the test passed", 0, "ok 3 - " + NAME + "\n# tests 4\n# fail 0\n", False),
    ("another test failed", 1, "not ok 1 - something else\n  code: 'ERR_ASSERTION'\n  error: 'another slot'\n# tests 1\n", False),
    ("a timeout", -9, tap, False),
]:
    caught, reason = mb.classify(node, rc, text, timed_out=(rc == -9))
    assert caught == want, f"node {name} counted as {'caught' if caught else 'not caught'} ({reason})"

TEST = "SceneStateTests.ASlotChangeResetsThatCustomerOnly"
unity = mb.unity(TEST, r"Expected: Stale")
xml = '<test-case id="1" fullname="' + TEST + '" result="Failed"><failure><message><![CDATA[  Expected: Stale\n  But was:  Invalid]]></message></failure></test-case>'
for name, rc, text, want in [
    ("the expected assertion", 1, xml, True),
    ("no NUnit result (a compile error)", 1, "error CS1002: ; expected", False),
    ("the test passed", 0, xml.replace('result="Failed"', 'result="Passed"'), False),
    ("another message", 1, xml.replace("Expected: Stale", "Expected: Applied"), False),
    ("another test failed", 1, xml.replace(TEST, "SceneStateTests.Other"), False),
]:
    caught, reason = mb.classify(unity, rc, text)
    assert caught == want, f"unity {name} counted as {'caught' if caught else 'not caught'} ({reason})"

assert mb.passed(node, 0, "# tests 8\n# fail 0\n") and not mb.passed(node, 0, "# tests 8\n# fail 1\n"), "node baseline rule"
assert mb.passed(unity, 0, xml.replace('result="Failed"', 'result="Passed"')) and not mb.passed(unity, 0, xml), "unity baseline rule"

root = os.path.dirname(os.path.dirname(here))
for name, t, edits in mb.ITEMS:
    for path, old, _ in edits:
        n = open(os.path.join(root, path)).read().count(old)
        assert n == 1, f"{name}: anchor in {path} found {n} times"
print(f"{len(mb.ITEMS)} items' anchors and the node and unity classifiers pass")
