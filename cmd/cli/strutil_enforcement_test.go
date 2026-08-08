package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestNoBareBlankChecksOnFlaggedStrings is the enforcement test committed to
// by the empty-check-lint-helper story: the == "" vs strings.TrimSpace(x) ==
// "" bug was independently reintroduced 4 times across Epic 4 (core/ble.go
// x2, cmd/cli/ble.go, and the BLE text writers' shared blerender.field), and
// caught only at code review each time, never by the initial implementation
// pass. This test mirrors TestDiagnosticFieldsOnlyReadInRenderDiagnostic's
// AST + go/types technique so the same class of bug fails a normal `go test
// ./...` run instead of depending on a reviewer noticing it again.
//
// Deliberately narrow: it does NOT ban every "== \"\"" in the repo. A
// blanket ban would false-positive on the ~25 legitimate bare == "" checks
// against internally-formatted invariant strings (net.IP.String(),
// net.HardwareAddr.String(), etc. in core/engine/merge.go, discovery/*,
// enrich/*) that can never be whitespace-only garbage, so trimming them
// would be a no-op. Instead this enumerates the specific (type, field) and
// (function, variable) pairs known to need strutil.IsBlank — the same
// enumerate-don't-infer approach the Diagnostic-field test already uses for
// AD-8 — and fails only if one of those specific sites reverts to a bare
// comparison.
func TestNoBareBlankChecksOnFlaggedStrings(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg,
		"nats/core/ble",
		"nats/cmd/cli",
		"nats/report/ble/internal/blerender",
	)
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	// Struct-field sites: keyed by (containing type, field name). Matched
	// like the Diagnostic-field test — by the field's owning type, not by
	// whatever a caller happens to name the receiving variable — so a
	// future site touching the same field is covered automatically.
	flaggedFields := map[string]string{
		"core/ble.BLEDeviceProfile": "Name",
		"core/ble.Advertisement":    "Name",
	}

	// Local variable/parameter sites: keyed by (package path, enclosing
	// function, variable name). These aren't struct fields, so they can
	// only be pinned down to the specific function they live in — matching
	// by bare variable name alone would flag unrelated locals elsewhere in
	// the module that happen to share a common name like "s" or "raw".
	type funcVar struct{ pkgPath, funcName, varName string }
	flaggedVars := []funcVar{
		{"nats/core/ble", "skipDiagnostic", "reason"},
		{"nats/cmd/cli", "resolveBLEFormat", "raw"},
		{"nats/report/ble/internal/blerender", "field", "s"},
	}

	var violations []string

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			t.Fatalf("package %s failed to load: %v", pkg.PkgPath, pkg.Errors)
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				funcName := fn.Name.Name

				ast.Inspect(fn, func(n ast.Node) bool {
					bin, ok := n.(*ast.BinaryExpr)
					if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) {
						return true
					}

					other, isEmptyStringCompare := emptyStringOperand(bin)
					if !isEmptyStringCompare {
						return true
					}

					switch expr := other.(type) {
					case *ast.Ident:
						obj := pkg.TypesInfo.Uses[expr]
						if _, ok := obj.(*types.Var); !ok {
							return true
						}
						for _, fv := range flaggedVars {
							if pkg.PkgPath == fv.pkgPath && funcName == fv.funcName && expr.Name == fv.varName {
								violations = append(violations, pkg.PkgPath+"."+funcName+" (bare check on "+expr.Name+")")
							}
						}

					case *ast.SelectorExpr:
						selection, ok := pkg.TypesInfo.Selections[expr]
						if !ok || selection.Recv() == nil {
							return true
						}
						recv := selection.Recv().String()
						for typeSubstr, field := range flaggedFields {
							if expr.Sel.Name == field && strings.Contains(recv, typeSubstr) {
								violations = append(violations, pkg.PkgPath+"."+funcName+" (bare check on ."+field+")")
							}
						}
					}

					return true
				})
			}
		}
	}

	if len(violations) != 0 {
		t.Fatalf("bare == \"\"/!= \"\" blank-check found on a flagged string field/variable; use strutil.IsBlank instead (the == \"\" vs strings.TrimSpace(x) == \"\" bug, reintroduced 4 times in Epic 4): %v", violations)
	}
}

// emptyStringOperand reports whether one side of bin is the literal empty
// string, returning the other (non-literal) operand when so.
func emptyStringOperand(bin *ast.BinaryExpr) (ast.Expr, bool) {
	if isEmptyStringLit(bin.Y) {
		return bin.X, true
	}
	if isEmptyStringLit(bin.X) {
		return bin.Y, true
	}
	return nil, false
}

func isEmptyStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	unquoted, err := strconv.Unquote(lit.Value)
	return err == nil && unquoted == ""
}
