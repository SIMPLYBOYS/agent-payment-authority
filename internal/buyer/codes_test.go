package buyer

// The code lists a run may leave with (internal/store RunOutcomes and
// RunDetails, docs/demo-operation-design.md 13.10-13.11) against the codes
// this package's source can produce, read with go/ast; and that unlisted
// codes and error text never reach the trace or the result.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

func parseFiles(t *testing.T, names ...string) []*ast.File {
	t.Helper()
	var out []*ast.File
	fset := token.NewFileSet()
	for _, n := range names {
		f, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	return out
}

func lit(e ast.Expr) (string, bool) {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// producedDetails are the detail codes host.go, runner.go and gate.go can
// set: stop(…, "x"), stopErr{…, "x"}, "input_"+status, the returns of the
// Watch closure and of Gate.check (with the plugins/skills/agents loop),
// and gateCode = "x" (not "passed", which only goes to the trace's gate).
func producedDetails(t *testing.T) []string {
	t.Helper()
	set := map[string]bool{}
	for _, f := range parseFiles(t, "host.go", "runner.go", "gate.go") {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "stop" && len(x.Args) == 2 {
					if s, ok := lit(x.Args[1]); ok {
						set[s] = true
					}
					if b, ok := x.Args[1].(*ast.BinaryExpr); ok {
						if p, ok := lit(b.X); ok && p == "input_" {
							set["input_"+InputFailed], set["input_"+InputPartial] = true, true
						}
					}
				}
			case *ast.CompositeLit:
				if id, ok := x.Type.(*ast.Ident); ok && id.Name == "stopErr" && len(x.Elts) == 2 {
					if s, ok := lit(x.Elts[1]); ok {
						set[s] = true
					}
				}
			case *ast.FuncDecl:
				if x.Name.Name == "check" { // Gate.check: every returned string
					ast.Inspect(x.Body, func(m ast.Node) bool {
						if r, ok := m.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
							if s, ok := lit(r.Results[0]); ok {
								set[s] = true
							}
						}
						if rg, ok := m.(*ast.RangeStmt); ok {
							if cl, ok := rg.X.(*ast.CompositeLit); ok {
								for _, e := range cl.Elts {
									if s, ok := lit(e); ok {
										set[s] = true
									}
								}
							}
						}
						return true
					})
				}
			case *ast.AssignStmt:
				if id, ok := x.Lhs[0].(*ast.Ident); ok && id.Name == "watch" {
					ast.Inspect(x.Rhs[0], func(m ast.Node) bool {
						if r, ok := m.(*ast.ReturnStmt); ok && len(r.Results) == 2 {
							if s, ok := lit(r.Results[1]); ok {
								set[s] = true
							}
						}
						return true
					})
				}
				if id, ok := x.Lhs[0].(*ast.Ident); ok && id.Name == "gateCode" && len(x.Rhs) == 1 {
					if s, ok := lit(x.Rhs[0]); ok && s != "passed" {
						set[s] = true
					}
				}
			}
			return true
		})
	}
	delete(set, "")
	var out []string
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// hostDetails are the listed codes cmd/demo adds (its own sync test) and
// the fixed values that come from no source.
var hostDetails = []string{"", "host_error", "scratch_home_failed", "task_reserved_by_run", "detail_unlisted"}

func TestDetailListMatchesTheSource(t *testing.T) {
	got := producedDetails(t)
	for _, d := range got {
		if !slices.Contains(store.RunDetails, d) {
			t.Errorf("the source produces %q, which store.RunDetails does not list", d)
		}
	}
	for _, d := range store.RunDetails {
		if !slices.Contains(hostDetails, d) && !slices.Contains(got, d) {
			t.Errorf("store.RunDetails lists %q, which this package no longer produces", d)
		}
	}
	for _, must := range []string{"tools", "permission_mode", "auth_unconfirmed", "plugins", "skills", "agents", "init_missing", "input_partial", "assistant_messages"} {
		if !slices.Contains(got, must) {
			t.Errorf("the scan misses %q: the scanner itself is wrong", must)
		}
	}
}

func TestOutcomeListMatchesTheSource(t *testing.T) {
	var consts []string
	for _, f := range parseFiles(t, "runner.go") {
		ast.Inspect(f, func(n ast.Node) bool {
			if v, ok := n.(*ast.ValueSpec); ok {
				if id, ok := v.Type.(*ast.Ident); ok && id.Name == "Outcome" && len(v.Values) == 1 {
					if s, ok := lit(v.Values[0]); ok {
						consts = append(consts, s)
					}
				}
			}
			return true
		})
	}
	if len(consts) < 15 {
		t.Fatalf("scan found %d outcomes", len(consts))
	}
	for _, c := range consts {
		if !slices.Contains(store.RunOutcomes, c) {
			t.Errorf("Outcome %q is not in store.RunOutcomes", c)
		}
	}
	for _, c := range store.RunOutcomes {
		if c != "skipped" && !slices.Contains(consts, c) {
			t.Errorf("store.RunOutcomes lists %q, which runner.go does not define", c)
		}
	}
}

// A synthetic marker, injected as an unlisted detail and as a code-less
// prepare error, appears in no trace file and in no code of the result.
func TestNoUnlistedTextLeaves(t *testing.T) {
	const mark = "APA-SECRET-MARK-7f3c"
	e := newHost(t)
	watchHook = func(typ, subtype string) (Outcome, string) {
		if typ == "system" && subtype == "init" {
			return ConfigMismatch, "x_" + mark
		}
		return "", ""
	}
	res := e.run(context.Background(), "task-a", "ask-mainline")
	watchHook = nil
	if res.Report.Detail != "detail_unlisted" {
		t.Fatalf("detail %q", res.Report.Detail)
	}
	res2 := e.runWith(context.Background(), "task-a-next", "ask-mainline", func() (*store.Store, error) { return nil, errors.New("boom " + mark) })
	if res2.Report.Detail != "prepare_failed" {
		t.Fatalf("detail %q", res2.Report.Detail)
	}
	found := false
	filepath.Walk(e.h.TraceDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), mark) {
				t.Errorf("%s holds the marker", p)
			}
			if strings.Contains(string(b), `"prepare_error":"STORE_ERROR"`) {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Error("no trace records prepare_error STORE_ERROR")
	}
}
