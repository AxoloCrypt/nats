package main

import (
	"go/ast"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestDiagnosticFieldsOnlyReadInRenderDiagnostic is the AD-8 enforcement test
// committed to during epic planning: every Diagnostic, regardless of whether
// core/engine, core/ble, or cmd/cli produced it, must be printed through
// renderDiagnostic. This uses type information (not just identifier names)
// to verify nothing else in these packages reads a Diagnostic's Severity,
// Message, or Reason field — i.e. nobody can bypass the shared renderer with
// a stray fmt.Println.
//
// Both Diagnostic types are checked. core/ble defines its own, structurally
// identical, type because NL-AD-1 forbids core/ble importing core/engine, so
// a check written against core/engine.Diagnostic alone would silently leave
// the entire BLE vertical unguarded — exactly the gap Story 4.7's review
// found, where an invented severity token and message shape in runBLEScan
// still passed this test.
//
// core/ble.Diagnostic has one additional sanctioned reader:
// renderBLEDiagnostic, which exists solely to convert it into the
// engine.Diagnostic that renderDiagnostic formats. That conversion is the
// single code path AC #2 requires, not a second renderer — it produces no
// output of its own.
func TestDiagnosticFieldsOnlyReadInRenderDiagnostic(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg, "nats/cmd/cli", "nats/core/engine", "nats/core/ble")
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	fieldNames := map[string]bool{"Severity": true, "Message": true, "Reason": true}

	// Diagnostic type -> the one function permitted to read its fields.
	allowedReader := map[string]string{
		"core/engine.Diagnostic": "nats/cmd/cli.renderDiagnostic",
		"core/ble.Diagnostic":    "nats/cmd/cli.renderBLEDiagnostic",
	}

	var readingFuncs []string
	seenTypes := map[string]int{}
	total := 0

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
					sel, ok := n.(*ast.SelectorExpr)
					if !ok || !fieldNames[sel.Sel.Name] {
						return true
					}
					selection, ok := pkg.TypesInfo.Selections[sel]
					if !ok || selection.Recv() == nil {
						return true
					}
					recv := selection.Recv().String()
					diagType := ""
					for candidate := range allowedReader {
						if strings.Contains(recv, candidate) {
							diagType = candidate
							break
						}
					}
					if diagType == "" {
						return true
					}
					total++
					seenTypes[diagType]++
					if pkg.PkgPath+"."+funcName != allowedReader[diagType] {
						readingFuncs = append(readingFuncs,
							pkg.PkgPath+"."+funcName+" (reads "+diagType+")")
					}
					return true
				})
			}
		}
	}

	if total == 0 {
		t.Fatal("expected at least one Diagnostic field read across cmd/cli, core/engine and core/ble — test setup is broken")
	}
	// Asserted per type, not just in aggregate: a single combined counter
	// would stay non-zero from the engine side alone even if the BLE half of
	// the check silently stopped matching anything.
	for diagType := range allowedReader {
		if seenTypes[diagType] == 0 {
			t.Fatalf("expected at least one %s field read — this guard is no longer covering that type", diagType)
		}
	}
	if len(readingFuncs) != 0 {
		t.Fatalf("Diagnostic fields must only be read inside their sanctioned renderer (AD-8); found reads in: %v", readingFuncs)
	}
}
