package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestNoBareBlankChecksOnFlaggedStrings exists because the == "" vs
// strings.TrimSpace(x) == "" bug was independently reintroduced 4 times
// across the BLE work (core/ble.go x2, cmd/cli/ble.go, and the BLE text
// writers' shared blerender.field), plus a 5th pre-existing instance in
// cmd/cli/root.go's buildOptions. Every one was caught only at code review,
// never by the initial implementation pass. This test mirrors
// TestDiagnosticFieldsOnlyReadInRenderDiagnostic's AST + go/types technique
// so the same class of bug fails a normal `go test ./...` run instead of
// depending on a reviewer noticing it again.
//
// Deliberately narrow: it does NOT ban every "== \"\"" in the repo. A
// blanket ban would false-positive on the ~25 legitimate bare == "" checks
// against internally-formatted invariant strings (net.IP.String(),
// net.HardwareAddr.String(), etc. in core/engine/merge.go, discovery/*,
// enrich/*) that can never be whitespace-only garbage, so trimming them
// would be a no-op. Instead this enumerates the specific (type, field) and
// (function, variable) pairs known to need strutil.IsBlank — the same
// enumerate-don't-infer approach the Diagnostic-field test already uses —
// and fails only if one of those specific sites reverts to a bare
// comparison.
//
// A bare-comparison scan alone has a blind spot: it can't distinguish "this
// site is correctly migrated" from "this site (or the whole guard) silently
// stopped existing" — both look identical, zero violations found. A code
// review of this test's first version found exactly that gap: renaming a
// flagged variable (e.g. resolveBLEFormat's raw -> rawFmt) while reverting
// to a bare comparison made the test pass cleanly. So, mirroring the
// precedent's total==0/seenTypes floor check, this test also positively
// tracks that each known site still calls strutil.IsBlank — and fails if any
// expected site is never observed, even when zero violations are found.
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
	// future site touching the same field is covered automatically. Typed
	// against the exact type string (not a substring match) so a future type
	// whose name merely contains "BLEDeviceProfile"/"Advertisement" (e.g. a
	// hypothetical BLEDeviceProfileSnapshot) can't be conflated with these.
	flaggedFields := map[string]string{
		"nats/core/ble.BLEDeviceProfile": "Name",
		"nats/core/ble.Advertisement":    "Name",
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
		{"nats/cmd/cli", "buildOptions", "format"},
		{"nats/report/ble/internal/blerender", "field", "s"},
	}

	// Known-good call sites: the same (function, variable)/(function, field)
	// pairs above, but tracked as "has this site been observed calling
	// strutil.IsBlank" rather than "has a bare comparison been found here".
	// This is the floor check — every entry must end up true.
	type knownFieldSite struct{ pkgPath, funcName, typeSubstr, field string }
	knownFieldSites := []knownFieldSite{
		{"nats/core/ble", "Run", "nats/core/ble.BLEDeviceProfile", "Name"},
		{"nats/core/ble", "Run", "nats/core/ble.Advertisement", "Name"},
	}
	fieldSiteSeen := make([]bool, len(knownFieldSites))
	varSiteSeen := make([]bool, len(flaggedVars))

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
					if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 1 && isStrutilIsBlankCall(pkg, call) {
						switch arg := call.Args[0].(type) {
						case *ast.Ident:
							for i, fv := range flaggedVars {
								if pkg.PkgPath == fv.pkgPath && funcName == fv.funcName && arg.Name == fv.varName {
									varSiteSeen[i] = true
								}
							}
						case *ast.SelectorExpr:
							if selection, ok := pkg.TypesInfo.Selections[arg]; ok && selection.Recv() != nil {
								recv := selection.Recv().String()
								for i, ks := range knownFieldSites {
									if pkg.PkgPath == ks.pkgPath && funcName == ks.funcName &&
										arg.Sel.Name == ks.field && typeMatches(recv, ks.typeSubstr) {
										fieldSiteSeen[i] = true
									}
								}
							}
						}
					}

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
							if expr.Sel.Name == field && typeMatches(recv, typeSubstr) {
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
		t.Fatalf("bare == \"\"/!= \"\" blank-check found on a flagged string field/variable; use strutil.IsBlank instead (the == \"\" vs strings.TrimSpace(x) == \"\" bug, reintroduced 4 times already): %v", violations)
	}

	for i, fv := range flaggedVars {
		if !varSiteSeen[i] {
			t.Fatalf("expected strutil.IsBlank(%s) inside %s.%s — this guard's coverage of that site was lost (renamed, refactored away, or the call itself removed) even though no bare comparison was found", fv.varName, fv.pkgPath, fv.funcName)
		}
	}
	for i, ks := range knownFieldSites {
		if !fieldSiteSeen[i] {
			t.Fatalf("expected strutil.IsBlank(....%s) inside %s.%s — this guard's coverage of that site was lost even though no bare comparison was found", ks.field, ks.pkgPath, ks.funcName)
		}
	}
}

// isStrutilIsBlankCall reports whether call invokes nats/internal/strutil's
// IsBlank, resolved via type info (not just the literal identifier "strutil")
// so an import alias can't defeat the check.
func isStrutilIsBlankCall(pkg *packages.Package, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "IsBlank" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pkgName, ok := pkg.TypesInfo.Uses[ident].(*types.PkgName)
	if !ok {
		return false
	}
	return pkgName.Imported().Path() == "nats/internal/strutil"
}

// typeMatches reports whether recv (a selection's receiver type string, e.g.
// "*nats/core/ble.BLEDeviceProfile" or "nats/core/ble.BLEDeviceProfile") is
// exactly typeSubstr, ignoring only a leading pointer "*" — not a substring
// match, so a future type whose name merely contains typeSubstr (e.g. a
// hypothetical BLEDeviceProfileSnapshot) isn't conflated with it.
func typeMatches(recv, typeSubstr string) bool {
	return recv == typeSubstr || recv == "*"+typeSubstr
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
