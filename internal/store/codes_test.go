package store

// StoreCodes against the codes this package's source refuses with: the
// first argument of refuse("X", …) and the code field of the
// {col, value, code} tables, read with go/ast (docs/demo-operation-design.md
// 13.10-13.11).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestStoreCodesMatchTheSource(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	set := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "refuse" && len(x.Args) == 2 {
					if b, ok := x.Args[0].(*ast.BasicLit); ok {
						s, _ := strconv.Unquote(b.Value)
						set[s] = true
					}
				}
			case *ast.CompositeLit: // []struct{ col, value, code string }{{…, …, "X"}, …}
				at, ok := x.Type.(*ast.ArrayType)
				if !ok {
					return true
				}
				st, ok := at.Elt.(*ast.StructType)
				if !ok {
					return true
				}
				idx, i := -1, 0
				for _, fl := range st.Fields.List {
					for _, nm := range fl.Names {
						if nm.Name == "code" {
							idx = i
						}
						i++
					}
				}
				for _, e := range x.Elts {
					if c, ok := e.(*ast.CompositeLit); ok && idx >= 0 && idx < len(c.Elts) {
						if b, ok := c.Elts[idx].(*ast.BasicLit); ok {
							s, _ := strconv.Unquote(b.Value)
							set[s] = true
						}
					}
				}
			}
			return true
		})
	}
	for c := range set {
		if !slices.Contains(StoreCodes, c) {
			t.Errorf("the source refuses with %q, which StoreCodes does not list", c)
		}
	}
	for _, c := range StoreCodes {
		if c != "STORE_ERROR" && !set[c] {
			t.Errorf("StoreCodes lists %q, which the source no longer uses", c)
		}
	}
	for _, must := range []string{"WALLET_ID_IN_USE", "PAYER_IN_USE", "NO_FREE_TASK"} {
		if !set[must] {
			t.Errorf("the scan misses %q: the scanner itself is wrong", must)
		}
	}
}
