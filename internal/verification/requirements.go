package verification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// requirements are docs/m1-closeout-gaps.md section 3.2 with the tests that
// show each (docs/m1-evidence.md section 7): the checker's own list, not
// the bundle's index. A test name ending in * is a prefix.
var requirements = []struct {
	name   string
	checks [][2]string // evidence file, test
}{
	{"a page refresh restores from the Store", [][2]string{{"browser/browser-ui.out", "refresh + sign-in restores task-a from the Store"}}},
	{"hostile strings are shown as text only", [][2]string{{"tests/requirements-web.tap", "hostile strings stay strings; nothing reads as paid"}, {"browser/browser-ui.out", "hostile strings render as text only"}}},
	{"a model saying it paid shows no success", [][2]string{{"browser/browser-ui.out", "nothing says paid or delivered"}, {"tests/requirements-go.jsonl", "TestLiveRun"}}},
	{"a resent event is not animated twice", [][2]string{{"tests/requirements-web.tap", "duplicate events are shown once and animated once"}}},
	{"switching A/B mixes no data", [][2]string{{"tests/requirements-web.tap", "a switch drops every late reply of the old viewer"}, {"tests/requirements-go.jsonl", "TestIntegrationAskDenyAndAccess"}}},
	{"cancel and CLI timeout have their own states", [][2]string{{"tests/requirements-go.jsonl", "TestCancel*"}, {"browser/browser-ui.out", "3.4:*"}}},
}

// testStatus reads one test's status from a log: go test -json lines
// (top-level tests), TAP (top-level "ok N - name", "not ok", "# SKIP") or
// browser-ui's "PASS name" / "FAIL name" lines. pass needs at least one
// passing match and no failing or skipped one.
func testStatus(file string, log []byte, name string) string {
	prefix := strings.HasSuffix(name, "*")
	name = strings.TrimSuffix(name, "*")
	match := func(s string) bool { return s == name || prefix && strings.HasPrefix(s, name) }
	pass, fail, skip := 0, 0, 0
	sc := bufio.NewScanner(bytes.NewReader(log))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasSuffix(file, ".jsonl"):
			var e struct{ Action, Test string }
			if json.Unmarshal([]byte(line), &e) != nil || strings.Contains(e.Test, "/") || !match(e.Test) {
				continue
			}
			switch e.Action {
			case "pass":
				pass++
			case "fail":
				fail++
			case "skip":
				skip++
			}
		case strings.HasSuffix(file, ".tap"):
			ok := strings.HasPrefix(line, "ok ")
			if !ok && !strings.HasPrefix(line, "not ok ") {
				continue // indented lines belong to subtests
			}
			_, rest, found := strings.Cut(line, " - ")
			if !found {
				continue
			}
			title, directive, _ := strings.Cut(rest, " # ")
			if !match(title) {
				continue
			}
			switch {
			case strings.HasPrefix(strings.ToUpper(directive), "SKIP") || strings.HasPrefix(strings.ToUpper(directive), "TODO"):
				skip++
			case ok:
				pass++
			default:
				fail++
			}
		default:
			if s, ok := strings.CutPrefix(line, "PASS "); ok && match(s) {
				pass++
			} else if s, ok := strings.CutPrefix(line, "FAIL "); ok && match(s) {
				fail++
			}
		}
	}
	switch {
	case fail > 0:
		return "fail"
	case skip > 0:
		return "skip"
	case pass > 0:
		return "pass"
	}
	return "missing"
}

// requirements checks each requirement from the test records themselves
// (R): a failing test fails it, a missing or skipped one leaves it
// INCONCLUSIVE, and the bundle's index saying pass where the record does
// not is a contradiction (FAIL).
func (v *verifier) requirements() {
	f := &v.f
	const index = "requirements/index.json"
	var listed []struct {
		Requirement string `json:"requirement"`
		Checks      []struct {
			Evidence string `json:"evidence"`
			Test     string `json:"test"`
			Status   string `json:"status"`
		} `json:"checks"`
	}
	claimed := map[[2]string]string{}
	if v.readJSON(f, "R", index, &listed) {
		for _, r := range listed {
			for _, c := range r.Checks {
				claimed[[2]string{c.Evidence, c.Test}] = c.Status
			}
		}
	}
	logs := map[string][]byte{}
	for _, r := range requirements {
		rr := RequirementReport{Requirement: r.name, Verdict: Pass}
		for _, c := range r.checks {
			log, ok := logs[c[0]]
			if !ok {
				raw, err := v.b.read(c[0])
				if err != nil {
					v.readErr(f, "R", c[0], err)
				}
				log, logs[c[0]] = raw, raw
			}
			st := testStatus(c[0], log, c[1])
			rr.Checks = append(rr.Checks, c[0]+": "+c[1]+": "+st)
			switch st {
			case "fail":
				rr.Verdict = worst(rr.Verdict, Fail)
				f.fail("R", []string{c[0]}, "%s: test %s failed", r.name, quote(c[1]))
			case "skip", "missing":
				rr.Verdict = worst(rr.Verdict, Inconclusive)
				f.missing("R", []string{c[0]}, "%s: test %s is %s in the record", r.name, quote(c[1]), st)
			}
			if said, ok := claimed[c]; ok && said == "pass" && st != "pass" {
				rr.Verdict = worst(rr.Verdict, Fail)
				f.fail("R", []string{index, c[0]}, "%s: the index says %s passed; the record shows %s", r.name, quote(c[1]), st)
			}
		}
		v.r.Requirements = append(v.r.Requirements, rr)
	}
}
